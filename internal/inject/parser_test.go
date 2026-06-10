package inject_test

import (
	"fmt"
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
		{"echo {{lympht:ns/path#key}}", true},
		{"{{lympht:a#b}} and {{lympht:c#d}}", true},
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
			"neunexus/foo#password": "secret123",
			"neunexus/bar#api-key":  "key456",
		},
	}

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "single placeholder",
			input: `vault kv put secret/x pass="{{lympht:neunexus/foo#password}}"`,
			want:  `vault kv put secret/x pass="secret123"`,
		},
		{
			name:  "multiple placeholders",
			input: `--pass={{lympht:neunexus/foo#password}} --key={{lympht:neunexus/bar#api-key}}`,
			want:  `--pass=secret123 --key=key456`,
		},
		{
			name:  "no placeholder passthrough",
			input: `echo hello world`,
			want:  `echo hello world`,
		},
		{
			name:    "missing field returns error",
			input:   `{{lympht:neunexus/foo#missing}}`,
			wantErr: true,
		},
	}

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
