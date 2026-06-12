package hook_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tojiuni/lympht/internal/hook"
)

type mockFetcher struct {
	data map[string]string
}

func (m *mockFetcher) GetField(path, field string) (string, error) {
	val, ok := m.data[path+"#"+field]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return val, nil
}

func TestRun_Passthrough(t *testing.T) {
	input := `{"tool_name":"Bash","tool_input":{"command":"echo hello"}}`
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, &mockFetcher{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No placeholder → no output (passthrough)
	if out.Len() != 0 {
		t.Errorf("expected empty output for passthrough, got: %q", out.String())
	}
}

func TestRun_SubstitutesPlaceholder(t *testing.T) {
	input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/foo#pass}}"}}`
	fetcher := &mockFetcher{data: map[string]string{"vault:ns/foo#pass": "secret99"}}

	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	hookOut, _ := resp["hookSpecificOutput"].(map[string]any)
	decision, _ := hookOut["permissionDecision"].(string)
	if decision != "allow" {
		t.Errorf("permissionDecision = %q, want %q", decision, "allow")
	}
	updatedInput, _ := hookOut["updatedInput"].(map[string]any)
	cmd, _ := updatedInput["command"].(string)
	if cmd != "echo 'secret99'" {
		t.Errorf("command = %q, want %q", cmd, "echo 'secret99'")
	}
}

func TestRun_NonBashToolPassthrough(t *testing.T) {
	input := `{"tool_name":"Read","tool_input":{"file_path":"/foo"}}`
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, &mockFetcher{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("non-Bash tool should passthrough, got output: %q", out.String())
	}
}

func TestRun_VaultErrorReturnsError(t *testing.T) {
	input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/missing#pass}}"}}`
	fetcher := &mockFetcher{data: map[string]string{}}
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, fetcher)
	if err == nil {
		t.Fatal("expected error when vault lookup fails")
	}
}
