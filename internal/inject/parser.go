package inject

import (
	"fmt"
	"regexp"
	"strings"
)

// re matches an optional immediately-adjacent quote + {{lympht:path#field}} + optional quote.
// Capturing groups: (1) openQuote, (2) path, (3) field, (4) closeQuote.
var re = regexp.MustCompile(`(['"]?)\{\{lympht:([^#}]+)#([^}]+)\}\}(['"]?)`)

// Fetcher retrieves a secret field from a secret store.
type Fetcher interface {
	GetField(path, field string) (string, error)
}

// HasPlaceholders reports whether cmd contains any lympht placeholders.
func HasPlaceholders(cmd string) bool {
	return re.MatchString(cmd)
}

// Substitute replaces all {{lympht:path#field}} tokens in cmd with values from
// fetcher. When the placeholder is surrounded by matching quotes (or has none),
// the surrounding quotes are stripped and the value is emitted as a
// shell-safe single-quoted string. When the placeholder is embedded inside a
// larger quoted string (mismatched adjacent chars), the surrounding chars are
// preserved and the raw value is inserted unchanged.
func Substitute(cmd string, fetcher Fetcher) (string, error) {
	var firstErr error
	result := re.ReplaceAllStringFunc(cmd, func(match string) string {
		if firstErr != nil {
			return match
		}
		parts := re.FindStringSubmatch(match)
		openQ, path, field, closeQ := parts[1], parts[2], parts[3], parts[4]
		val, err := fetcher.GetField(path, field)
		if err != nil {
			firstErr = fmt.Errorf("lympht: resolving %s#%s: %w", path, field, err)
			return match
		}
		// Matching quotes (including both absent) → emit shell-safe single-quoted value.
		if openQ == closeQ {
			return shellQuote(val)
		}
		// Embedded inside a larger quoted string → preserve surrounding chars, raw value.
		return openQ + val + closeQ
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
