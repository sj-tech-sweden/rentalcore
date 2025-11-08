package pdf

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var discountKeywords = []string{
	"rabatt",
	"discount",
	"nachlass",
	"skonto",
	"abzug",
	"ermäßigung",
	"ermassigung",
	"preisnachlass",
	"sonderpreis",
	"gutschrift",
}

var finalPriceKeywords = []string{
	"endpreis",
	"finalpreis",
	"gesamtpreis",
	"nach rabatt",
	"rabattpreis",
	"rabattiert",
	"nettopreis",
	"endbetrag",
}

var amountTokenRegex = regexp.MustCompile(`[-+]?\s*(?:\d{1,3}(?:[.\s]\d{3})+|\d+)(?:[,\.]\d{2})`)
var fallbackAmountTokenRegex = regexp.MustCompile(`[-+]?\s*\d{2,}`)
var percentageRegex = regexp.MustCompile(`[-+]?\s*\d{1,3}(?:[.,]\d+)?\s*%`)

func findAmountToken(line string) (string, bool) {
	if token, ok := findTokenWithRegex(line, amountTokenRegex); ok {
		return token, true
	}
	return findTokenWithRegex(line, fallbackAmountTokenRegex)
}

func findDecimalAmountToken(line string) (string, bool) {
	return findTokenWithRegex(line, amountTokenRegex)
}

func findTokenWithRegex(line string, rx *regexp.Regexp) (string, bool) {
	indexes := rx.FindAllStringIndex(line, -1)
	if len(indexes) == 0 {
		return "", false
	}
	for i := len(indexes) - 1; i >= 0; i-- {
		idx := indexes[i]
		rest := strings.TrimLeft(line[idx[1]:], " \t")
		if len(rest) > 0 {
			next, _ := utf8.DecodeRuneInString(rest)
			if next == '%' {
				continue
			}
		}
		candidate := strings.TrimSpace(line[idx[0]:idx[1]])
		if candidate == "" {
			continue
		}
		return candidate, true
	}
	return "", false
}

func findPercentage(line string) (float64, bool) {
	indexes := percentageRegex.FindAllStringIndex(line, -1)
	if len(indexes) == 0 {
		return 0, false
	}
	idx := indexes[len(indexes)-1]
	token := strings.TrimSpace(line[idx[0]:idx[1]])
	token = strings.TrimSuffix(token, "%")
	token = strings.ReplaceAll(token, " ", "")
	token = strings.ReplaceAll(token, ",", ".")
	if token == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
