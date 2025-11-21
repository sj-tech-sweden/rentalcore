-- Remove total fields from pdf_extractions table
ALTER TABLE pdf_extractions
    DROP COLUMN parsed_total,
    DROP COLUMN discount_percent;
