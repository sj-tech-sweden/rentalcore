package pdf

import (
	"regexp"
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

var amountTokenRegex = regexp.MustCompile(`[-+]?\s*(?:\d{1,3}(?:[.\s]\d{3})+|\d+)(?:[,\.]\d{2})`)
var fallbackAmountTokenRegex = regexp.MustCompile(`[-+]?\s*\d{2,}`)

func findAmountToken(line string) (string, bool) {
	if token, ok := findTokenWithRegex(line, amountTokenRegex); ok {
		return token, true
	}
	return findTokenWithRegex(line, fallbackAmountTokenRegex)
}

func findTokenWithRegex(line string, rx *regexp.Regexp) (string, bool) {
	indexes := rx.FindAllStringIndex(line, -1)
	if len(indexes) == 0 {
		return "", false
	}
	for i := len(indexes) - 1; i >= 0; i-- {
		idx := indexes[i]
		if idx[1] < len(line) {
			next, _ := utf8.DecodeRuneInString(line[idx[1]:])
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
