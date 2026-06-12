package inject_test

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/tojiuni/lympht/internal/inject"
)

type mockFetcher struct {
	data map[string]string
	err  error
}

func (m *mockFetcher) GetField(path, field string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	key := path + "#" + field
	val, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("not found: %s#%s", path, field)
	}
	return val, nil
}

func TestHasPlaceholders(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"echo hello", false},
		{"echo {{lympht:vault:ns/path#key}}", true},
		{"{{lympht:vault:a#b}} and {{lympht:vault:c#d}}", true},
		{"{{notlympht:a#b}}", false},
	}
	for _, c := range cases {
		got := inject.HasPlaceholders(c.input)
		if got != c.want {
			t.Errorf("HasPlaceholders(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestSubstitute(t *testing.T) {
	fetcher := &mockFetcher{
		data: map[string]string{
			"vault:neunexus/foo#password": "secret123",
			"vault:neunexus/bar#api-key":  "key456",
		},
	}

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			// matching double-quotes stripped, value shell-quoted
			name:  "double-quoted placeholder",
			input: `vault kv put secret/x pass="{{lympht:vault:neunexus/foo#password}}"`,
			want:  `vault kv put secret/x pass='secret123'`,
		},
		{
			// unquoted placeholders get shell-quoted
			name:  "unquoted placeholders",
			input: `--pass={{lympht:vault:neunexus/foo#password}} --key={{lympht:vault:neunexus/bar#api-key}}`,
			want:  `--pass='secret123' --key='key456'`,
		},
		{
			// single-quoted placeholder — common env-var pattern
			name:  "single-quoted placeholder",
			input: `PGPASSWORD='{{lympht:vault:neunexus/foo#password}}' psql -U gopedia`,
			want:  `PGPASSWORD='secret123' psql -U gopedia`,
		},
		{
			// value with special chars gets properly shell-escaped
			name: "special chars in value",
			input: `PGPASSWORD={{lympht:vault:neunexus/foo#password}} psql`,
			want:  `PGPASSWORD='secret123' psql`,
		},
		{
			// embedded inside larger double-quoted string — raw substitution preserved
			name:  "embedded in larger quoted string",
			input: `curl -H "Authorization: Bearer {{lympht:vault:neunexus/bar#api-key}}"`,
			want:  `curl -H "Authorization: Bearer key456"`,
		},
		{
			name:  "no placeholder passthrough",
			input: `echo hello world`,
			want:  `echo hello world`,
		},
		{
			name:    "missing field returns error",
			input:   `{{lympht:vault:neunexus/foo#missing}}`,
			wantErr: true,
		},
	}

	// Verify single-quote escaping in value
	t.Run("single quote in value", func(t *testing.T) {
		f := &mockFetcher{data: map[string]string{"vault:p#k": "it's"}}
		got, err := inject.Substitute(`PGPASSWORD={{lympht:vault:p#k}}`, f)
		if err != nil {
			t.Fatal(err)
		}
		want := `PGPASSWORD='it'\''s'`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})


	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := inject.Substitute(c.input, fetcher)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestSubstituteSpecialCharPasswords verifies that passwords containing shell
// special characters survive substitution intact. Each case checks:
//  1. The substituted command string has the expected form.
//  2. bash actually evaluates the quoted value back to the original password
//     (round-trip via `bash -c 'printf %s "$PGPASSWORD"'`).
func TestSubstituteSpecialCharPasswords(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantCmd  string
	}{
		{
			name:     "dollar sign",
			password: "$ecret123",
			wantCmd:  `PGPASSWORD='$ecret123' psql`,
		},
		{
			name:     "space",
			password: "pass word",
			wantCmd:  `PGPASSWORD='pass word' psql`,
		},
		{
			name:     "double quote",
			password: `pass"word`,
			wantCmd:  `PGPASSWORD='pass"word' psql`,
		},
		{
			name:     "backslash",
			password: `pass\word`,
			wantCmd:  `PGPASSWORD='pass\word' psql`,
		},
		{
			name:     "backtick",
			password: "pass`word",
			wantCmd:  "PGPASSWORD='pass`word' psql",
		},
		{
			name:     "exclamation",
			password: "pass!word",
			wantCmd:  `PGPASSWORD='pass!word' psql`,
		},
		{
			name:     "at-and-hash",
			password: "p@ss#123",
			wantCmd:  `PGPASSWORD='p@ss#123' psql`,
		},
		{
			name:     "single quote",
			password: "it's",
			wantCmd:  `PGPASSWORD='it'\''s' psql`,
		},
		{
			name:     "multiple single quotes",
			password: "a'b'c",
			wantCmd:  `PGPASSWORD='a'\''b'\''c' psql`,
		},
		{
			name:     "combined special chars",
			password: `$P@ss"w\rd!'`,
			wantCmd:  `PGPASSWORD='$P@ss"w\rd!'\''' psql`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &mockFetcher{data: map[string]string{"vault:p#pw": c.password}}
			got, err := inject.Substitute(`PGPASSWORD={{lympht:vault:p#pw}} psql`, f)
			if err != nil {
				t.Fatalf("Substitute error: %v", err)
			}
			if got != c.wantCmd {
				t.Errorf("command string mismatch\n got: %q\nwant: %q", got, c.wantCmd)
			}

			// Round-trip: run the PGPASSWORD assignment through bash and read it back.
			// Semicolon separates assignment from printf so $PGPASSWORD is expanded
			// after the variable is set (not before, as with `VAR=val cmd` form).
			assignment := got[:len(got)-len(" psql")] // strip trailing " psql"
			bashCmd := assignment + `; printf '%s' "$PGPASSWORD"`
			out, err := exec.Command("sh", "-c", bashCmd).Output()
			if err != nil {
				t.Fatalf("bash round-trip failed: %v", err)
			}
			if string(out) != c.password {
				t.Errorf("bash round-trip mismatch\n got: %q\nwant: %q", string(out), c.password)
			}
		})
	}
}
