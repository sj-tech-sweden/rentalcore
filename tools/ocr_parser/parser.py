#!/usr/bin/env python3
"""OCR parser CLI that converts flattened invoice text into structured JSON."""
from __future__ import annotations

import json
import re
import sys
from dataclasses import dataclass, asdict
from typing import List, Optional

import click

UNIT_KEYWORDS = [
    "stück",
    "stk",
    "st",
    "set",
    "sets",
    "pcs",
    "person",
    "personen",
    "tag",
    "tage",
    "std",
    "stunden",
    "hour",
    "hours",
]

TOTAL_STOP_WORDS = (
    "gesamtbetrag",
    "bruttosumme",
    "zahlung",
    "wir freuen",
    "vielen dank",
    "zwischensumme",
    "rechnungsnr",
    "kundennr",
    "lieferdatum",
    "iban",
    "bic",
    "seite ",
    "tsunami events",
)

HEADER_KEYWORDS = ("bezeichnung", "menge", "einheit")

NUMBER_RE = re.compile(r"[-+]?\d+(?:[.,]\d+)?")
QUANTITY_UNIT_SPLIT_RE = re.compile(
    r"(\d+)\s*(?:x|mal)?\s*(" + "|".join(UNIT_KEYWORDS) + r")",
    re.IGNORECASE,
)


@dataclass
class ParsedItem:
    line_number: int
    description: str
    quantity: float
    unit: Optional[str]
    unit_price: float
    discount_percent: float
    line_total: float

    def to_dict(self) -> dict:
        return asdict(self)


@dataclass
class DocumentTotals:
    """Document-level totals and discounts."""
    subtotal: Optional[float] = None  # Last Zwischensumme before discount
    discount_amount: Optional[float] = None  # Total discount amount
    discount_percent: Optional[float] = None  # Total discount percentage
    total: Optional[float] = None  # Final Gesamtbetrag after discount

    def to_dict(self) -> dict:
        return {k: v for k, v in asdict(self).items() if v is not None}


class OCRParser:
    def __init__(self, raw_text: str) -> None:
        self.raw_text = raw_text

    def preprocess(self) -> List[str]:
        text = self.raw_text.replace("\r", "")
        # Fix escaped newlines if they exist (from database storage)
        if "\\n" in text and text.count("\\n") > text.count("\n"):
            text = text.replace("\\n", "\n")
        # Insert line breaks before quantity+unit blocks to split descriptor from numeric rows.
        pattern = re.compile(
            r"(\d+)\s+(" + "|".join(UNIT_KEYWORDS) + r")",
            flags=re.IGNORECASE,
        )
        text = pattern.sub(r"\n\1 \2", text)
        # Normalize spaces without touching newlines
        text = re.sub(r"[ \t]+", " ", text)
        # Restore newlines around known keywords
        text = text.replace("Pos. Bezeichnung", "Pos.Bezeichnung")
        lines = [line.strip() for line in text.split("\n")]
        return [line for line in lines if line]

    def parse(self) -> List[ParsedItem]:
        lines = self.preprocess()
        start_idx = self._find_table_start(lines)
        if start_idx == -1:
            return []

        segments: List[dict] = []
        current: Optional[dict] = None

        for line in lines[start_idx:]:
            lower = line.lower()

            if self._is_stop_line(lower):
                if current:
                    segments.append(current)
                    current = None
                if "gesamtbetrag" in lower:
                    break
                continue

            if self._looks_like_header(line) or lower.startswith("übertrag"):
                continue

            # Check if line is ONLY a position number (1-100)
            only_num_match = re.match(r"^([1-9]|[1-9][0-9]|100)$", line)
            if only_num_match:
                line_number = int(only_num_match.group(1))
                
                # Check if this could be a quantity instead of a new position
                if current and self._is_item_incomplete(current):
                    # Item is incomplete (not enough numeric values), treat as quantity
                    current["numeric_parts"].append(line)
                    continue
                
                # Item is complete or we don't have one, this is a new position
                if current:
                    segments.append(current)

                current = {
                    "line_number": line_number,
                    "description_parts": [],
                    "numeric_parts": [],
                }
                continue

            # Check for "number word" pattern (like "6 Personal", "9 Personal") - but be very strict
            # Only match if word is short (not a long description) and alphanumeric only
            pos_text_match = re.match(r"^([1-9]|[1-9][0-9]|100)\s+([A-Za-zäöüÄÖÜß]{3,15})$", line)
            if pos_text_match:
                line_number = int(pos_text_match.group(1))
                remainder = pos_text_match.group(2).strip()

                # If we have a current item, save it first
                if current:
                    segments.append(current)

                current = {
                    "line_number": line_number,
                    "description_parts": [remainder],
                    "numeric_parts": [],
                }
                continue

            if current is None:
                continue

            # We have a current item, check if this is numeric or description
            if self._is_numeric_value(line):
                current["numeric_parts"].append(line)
            else:
                # Skip standalone unit keywords (like "Stück", "Tag") - they belong to quantity, not description
                if line.lower() in [kw.lower() for kw in UNIT_KEYWORDS]:
                    continue

                # Only add text lines if the item is not yet complete
                # A complete item has description + enough numeric values
                if self._is_item_incomplete(current) or not current["description_parts"]:
                    current["description_parts"].append(line)
                # else: ignore this line (likely a section header between items)

        if current:
            segments.append(current)

        items: List[ParsedItem] = []
        for row in segments:
            item = self._finalize_row(row)
            if item:
                items.append(item)

        return items

    def _is_item_incomplete(self, item: dict) -> bool:
        """Check if item doesn't have enough numeric values yet (needs more data)."""
        numeric_parts = item.get("numeric_parts", [])
        # Count decimal numbers (prices)
        decimal_count = sum(1 for part in numeric_parts if re.search(r"\d+,\d{2}", part))
        # Item is incomplete if it has less than 2 decimal numbers (need at least unit_price + line_total)
        return decimal_count < 2

    def _is_stop_line(self, lower: str) -> bool:
        return any(stop in lower for stop in TOTAL_STOP_WORDS)

    def _find_table_start(self, lines: List[str]) -> int:
        """Find the start of the invoice items table."""
        # First try: look for a line with all header keywords (compact layout)
        for idx, line in enumerate(lines):
            lower = line.lower()
            if all(keyword in lower for keyword in HEADER_KEYWORDS):
                return idx + 1

        # Second try: look for header keywords in consecutive lines (column layout)
        for idx in range(len(lines) - 2):
            window = " ".join(lines[idx:idx+5]).lower()
            if all(keyword in window for keyword in HEADER_KEYWORDS):
                # Find the first line that is ONLY a number (1-100) after the header
                for j in range(idx, min(idx + 10, len(lines))):
                    if re.match(r"^([1-9]|[1-9][0-9]|100)$", lines[j]):
                        return j
                return idx + 5

        return 0

    def _looks_like_header(self, line: str) -> bool:
        lower = line.lower()
        return "pos." in lower and "bezeichnung" in lower

    def _is_numeric_value(self, line: str) -> bool:
        """Check if line is a numeric value (single decimal number or quantity+unit)."""
        stripped = line.strip()

        # Quantity + unit (e.g., "1 Stück") - check this FIRST
        if re.match(r"^\d+\s+(" + "|".join(UNIT_KEYWORDS) + r")$", stripped, re.IGNORECASE):
            return True

        # Single decimal number (e.g., "130,00" or just "1")
        if re.match(r"^\d+(?:,\d{2})?$", stripped):
            return True

        # Multiple decimal numbers on one line (original compact format)
        if len(re.findall(r"\d+,\d{2}", stripped)) >= 2:
            return True

        return False

    def _finalize_row(self, row: dict) -> Optional[ParsedItem]:
        numeric_blob = " ".join(row.get("numeric_parts", []))
        if not numeric_blob:
            return None
        numeric_blob = re.sub(r"(,\d{2})(?=\d)", r"\1 ", numeric_blob)
        tokens = self._extract_numbers(numeric_blob)
        if not tokens:
            return None

        unit = None
        numbers = tokens.copy()

        qty_match = QUANTITY_UNIT_SPLIT_RE.search(numeric_blob)
        if qty_match:
            quantity = float(qty_match.group(1))
            unit = qty_match.group(2).lower()
            if numbers and abs(numbers[0] - quantity) < 0.0001:
                numbers = numbers[1:]
        elif re.match(r"^\d+\s", numeric_blob):
            quantity = float(re.match(r"^(\d+)", numeric_blob).group(1))
            if numbers:
                numbers = numbers[1:]
        else:
            quantity = 1.0

        if len(numbers) >= 3:
            unit_price = numbers[0]
            discount_percent = numbers[1]
            line_total = numbers[2]
        elif len(numbers) == 2:
            unit_price = numbers[0]
            discount_percent = 0.0
            line_total = numbers[1]
        elif len(numbers) == 1:
            unit_price = numbers[0] / max(quantity, 1)
            discount_percent = 0.0
            line_total = numbers[0]
        else:
            return None

        description = " ".join(row.get("description_parts", [])).strip()
        if not description:
            description = numeric_blob.strip()

        return ParsedItem(
            line_number=row.get("line_number", 0),
            description=description,
            quantity=quantity or 1.0,
            unit=unit,
            unit_price=float(unit_price),
            discount_percent=float(discount_percent),
            line_total=float(line_total),
        )

    def _extract_numbers(self, line: str) -> List[float]:
        numbers = []
        for match in NUMBER_RE.finditer(line):
            token = match.group(0)
            value = self._to_float(token)
            if value is not None:
                numbers.append(value)
        return numbers

    def _to_float(self, token: str) -> Optional[float]:
        clean = token.replace(" ", "")
        clean = clean.replace(".", "").replace(",", ".")
        try:
            return float(clean)
        except ValueError:
            return None

    def parse_totals(self, lines: List[str]) -> DocumentTotals:
        """Extract document-level totals and discounts from the text."""
        totals = DocumentTotals()
        last_zwischensumme = None

        for i, line in enumerate(lines):
            lower = line.lower()

            # Extract Zwischensumme (netto) / Subtotal
            if "zwischensumme" in lower and "netto" in lower:
                # Look for amount on same line or next line
                numbers = self._extract_numbers(line)
                if numbers:
                    last_zwischensumme = numbers[-1]
                elif i + 1 < len(lines):
                    numbers = self._extract_numbers(lines[i + 1])
                    if numbers:
                        last_zwischensumme = numbers[-1]

            # Extract discount: "abzgl. 20,00 % Rabatt" or "abzgl. 20%" followed by amount
            if "abzgl" in lower or "rabatt" in lower:
                # Try to extract discount percentage
                percent_match = re.search(r"(\d+(?:,\d+)?)\s*%", line)
                if percent_match:
                    totals.discount_percent = self._to_float(percent_match.group(1))

                # Try to extract discount amount (negative number or on next line)
                numbers = self._extract_numbers(line)
                if numbers:
                    # Discount amount is usually negative or the last number
                    totals.discount_amount = abs(numbers[-1])
                elif i + 1 < len(lines):
                    numbers = self._extract_numbers(lines[i + 1])
                    if numbers:
                        totals.discount_amount = abs(numbers[-1])

            # Extract Gesamtbetrag / Total
            if "gesamtbetrag" in lower or "gesamt" in lower:
                # Skip "Gesamt €" column header
                if "bezeichnung" in lower or "menge" in lower:
                    continue

                numbers = self._extract_numbers(line)
                if numbers:
                    totals.total = numbers[-1]
                elif i + 1 < len(lines):
                    numbers = self._extract_numbers(lines[i + 1])
                    if numbers:
                        totals.total = numbers[-1]

        # Set subtotal to the last Zwischensumme found
        if last_zwischensumme:
            totals.subtotal = last_zwischensumme

        # If no subtotal found but we have a total, use items to calculate
        # This will be handled by the caller if needed

        return totals

def parse_input_payload(raw: str) -> str:
    raw = raw.strip()
    if not raw:
        return ""
    try:
        payload = json.loads(raw)
        if isinstance(payload, dict) and "raw_text" in payload:
            return str(payload["raw_text"])
    except json.JSONDecodeError:
        pass
    return raw


@click.command()
@click.option("--input", "input_path", type=click.Path(exists=True), help="Optional text file path.")
@click.option("--output", "output_path", type=click.Path(), help="Optional output JSON file.")
@click.option("--pretty", is_flag=True, help="Pretty-print JSON output.")
def cli(input_path: Optional[str], output_path: Optional[str], pretty: bool) -> None:
    """Parse OCR text into structured JSON."""
    if input_path:
        with open(input_path, "r", encoding="utf-8") as handle:
            raw_text = handle.read()
    else:
        raw_text = sys.stdin.read()
    raw_text = parse_input_payload(raw_text)
    parser = OCRParser(raw_text)

    # Parse items
    items = parser.parse()

    # Parse document totals
    lines = parser.preprocess()
    totals = parser.parse_totals(lines)

    # If no subtotal found, calculate from items
    if not totals.subtotal and items:
        calculated_total = sum(item.line_total for item in items)
        totals.subtotal = calculated_total

    # If no total found, calculate from subtotal and discount
    if not totals.total:
        if totals.subtotal and totals.discount_amount:
            totals.total = totals.subtotal - totals.discount_amount
        elif totals.subtotal:
            totals.total = totals.subtotal

    result = {
        "document": totals.to_dict(),
        "items": [item.to_dict() for item in items],
        "warnings": [],
    }
    dump = json.dumps(result, indent=2 if pretty else None)
    if output_path:
        with open(output_path, "w", encoding="utf-8") as handle:
            handle.write(dump)
    else:
        sys.stdout.write(dump)


if __name__ == "__main__":  # pragma: no cover
    cli()
