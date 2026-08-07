// Package guard hard-blocks Bash command SHAPES that are known to print
// secret values directly to stdout — independent of whether the command
// contains any {{lympht:...}} placeholder at all. Most real incidents
// (a GitHub PAT dumped via `kubectl get secret -o yaml`, a token embedded in
// a git remote URL) never touch lympht's placeholder syntax, so the
// substitution-failure fix in package hook can't catch them — they need to
// be blocked on sight, before the command ever reaches a shell.
//
// This is a command-TEXT-shape check only: it never inspects, fetches, or
// reasons about any actual secret value. It also isn't a hard security
// boundary against a deliberately adversarial agent (a base64-obfuscated or
// script-file-wrapped command can still slip past a text match) — it exists
// to catch the accidental, careless case, which is what both real incidents
// this session actually were.
package guard

import (
	"fmt"
	"regexp"
)

type rule struct {
	re     *regexp.Regexp
	reason string
}

var rules = []rule{
	{
		re: regexp.MustCompile(`(?i)\bkubectl\b[^|;\n]*\bget\s+secrets?\b[^|;\n]*(-o|--output)[= ]+(yaml|json)\b`),
		reason: "kubectl get secret -o yaml/json prints the FULL secret (every key, not just the one you need) to stdout. " +
			"Use -o jsonpath='{.data.KEY}' targeting one field, redirected straight to a file — never printed — " +
			"or ask the user to run it directly with the '!' prefix.",
	},
	{
		re: regexp.MustCompile(`(?i)\bvault\s+kv\s+get\b`),
		reason: "vault kv get prints the secret value directly to stdout. Use 'lympht check <path>' to list field " +
			"names (no values), a {{lympht:vault:...}} placeholder to use a value without seeing it, " +
			"or ask the user to run it directly with the '!' prefix.",
	},
	{
		re: regexp.MustCompile(`(?i)\blympht\s+inject\b`),
		reason: "lympht inject prints the resolved (plaintext) command to stdout by design — it exists for a human " +
			"to preview manually, not for an agent to run. Use the {{lympht:...}} placeholder directly in the " +
			"command instead; the PreToolUse hook substitutes it before execution without ever showing the value.",
	},
	{
		re: regexp.MustCompile(`://[^\s/:@'"]+:[^\s/@'"]+@`),
		reason: "credentials embedded directly in a URL (user:pass@host) will appear verbatim in this command and " +
			"its output/history. Use a GIT_ASKPASS helper or a credential file read at execution time instead.",
	},
}

// CheckCommand returns a non-nil error describing why cmd is blocked, or nil
// if it matches no known secret-exposure pattern.
func CheckCommand(cmd string) error {
	for _, r := range rules {
		if r.re.MatchString(cmd) {
			return fmt.Errorf("lympht: blocked — %s", r.reason)
		}
	}
	return nil
}
