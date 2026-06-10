package inject

import (
	"fmt"
	"regexp"
)

// re matches {{lympht:path#field}} placeholders.
var re = regexp.MustCompile(`\{\{lympht:([^#}]+)#([^}]+)\}\}`)

// Fetcher retrieves a secret field from a secret store.
type Fetcher interface {
	GetField(path, field string) (string, error)
}

// HasPlaceholders reports whether cmd contains any lympht placeholders.
func HasPlaceholders(cmd string) bool {
	return re.MatchString(cmd)
}

// Substitute replaces all {{lympht:path#field}} tokens in cmd with values
// from fetcher. Returns an error if any placeholder cannot be resolved.
func Substitute(cmd string, fetcher Fetcher) (string, error) {
	var firstErr error
	result := re.ReplaceAllStringFunc(cmd, func(match string) string {
		if firstErr != nil {
			return match
		}
		parts := re.FindStringSubmatch(match)
		path, field := parts[1], parts[2]
		val, err := fetcher.GetField(path, field)
		if err != nil {
			firstErr = fmt.Errorf("lympht: resolving %s#%s: %w", path, field, err)
			return match
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}
