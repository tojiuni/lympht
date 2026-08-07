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

// withTempState points LYMPHT_STATE_DIR at a fresh temp dir for the
// duration of the test — RunWithFetcher now always consults package scope's
// persistent active-provider state, so every test needs isolation from both
// the real user cache dir and from each other.
func withTempState(t *testing.T) {
	t.Helper()
	t.Setenv("LYMPHT_STATE_DIR", t.TempDir())
}

func TestRun_Passthrough(t *testing.T) {
	withTempState(t)
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
	withTempState(t)
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
	withTempState(t)
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
	withTempState(t)
	input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/missing#pass}}"}}`
	fetcher := &mockFetcher{data: map[string]string{}}
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, fetcher)
	if err == nil {
		t.Fatal("expected error when vault lookup fails")
	}
}

// TestRun_BlocksDangerousCommandEvenWithoutPlaceholder guards the actual
// incidents this feature exists for: a raw `kubectl get secret -o yaml`
// contains no {{lympht:...}} placeholder at all, so the substitution path
// never used to see it — it always fell through to plain passthrough. The
// guard check must run for every Bash command, independent of placeholders.
func TestRun_BlocksDangerousCommandEvenWithoutPlaceholder(t *testing.T) {
	withTempState(t)
	input := `{"tool_name":"Bash","tool_input":{"command":"kubectl get secret github-credentials -n tekton-pipelines -o yaml"}}`
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, &mockFetcher{})
	if err == nil {
		t.Fatal("expected the dangerous command to be blocked")
	}
	if out.Len() != 0 {
		t.Errorf("blocked command must not write a hookResponse, got: %q", out.String())
	}
}

// TestRun_AllowsBenignCommandUnaffectedByGuard is the regression guard for
// the guard check itself: it must not start blocking ordinary commands that
// happen to contain the word "secret" in a way that isn't the dangerous
// shape guard actually targets (kubectl get secret(s) -o yaml/json). This
// command is also deliberately NOT a scope core-action shape (describe, not
// get secret), so it stays allowed regardless of any active provider.
func TestRun_AllowsBenignCommandUnaffectedByGuard(t *testing.T) {
	withTempState(t)
	input := `{"tool_name":"Bash","tool_input":{"command":"kubectl describe pod secret-manager-7f8b -n iam"}}`
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, &mockFetcher{})
	if err != nil {
		t.Fatalf("unexpected error for a plain describe mentioning 'secret' in a pod name: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected empty output for passthrough, got: %q", out.String())
	}
}

// TestRun_BlocksCoreActionWithoutActiveProvider is the hook-level regression
// guard for package scope's integration: a plain `kubectl get secret` with
// no active tokenhub elevation must now be blocked end-to-end through
// RunWithFetcher, not just at the package-scope unit-test level.
func TestRun_BlocksCoreActionWithoutActiveProvider(t *testing.T) {
	withTempState(t)
	input := `{"tool_name":"Bash","tool_input":{"command":"kubectl get secret iam-db -n iam"}}`
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, &mockFetcher{})
	if err == nil {
		t.Fatal("expected kubectl get secret to be blocked with no active elevation")
	}
	if out.Len() != 0 {
		t.Errorf("blocked command must not write a hookResponse, got: %q", out.String())
	}
}

// TestRun_AllowsCoreActionAfterProviderRecorded confirms that once a prior
// Bash command in the same session requested the matching tokenhub
// provider, RunWithFetcher stops blocking the gated action — the two
// checks share state via package scope's persisted file, not just within a
// single call.
func TestRun_AllowsCoreActionAfterProviderRecorded(t *testing.T) {
	withTempState(t)

	credInput := `{"tool_name":"Bash","tool_input":{"command":"curl $TOKENHUB/v1/credentials -d '{\"provider\":\"k8s-secret-inspector\"}'"}}`
	var credOut strings.Builder
	if err := hook.RunWithFetcher(strings.NewReader(credInput), &credOut, &mockFetcher{}); err != nil {
		t.Fatalf("unexpected error recording the tokenhub credential fetch: %v", err)
	}

	input := `{"tool_name":"Bash","tool_input":{"command":"kubectl get secret iam-db -n iam"}}`
	var out strings.Builder
	err := hook.RunWithFetcher(strings.NewReader(input), &out, &mockFetcher{})
	if err != nil {
		t.Errorf("expected kubectl get secret to be allowed once k8s-secret-inspector was recorded, got: %v", err)
	}
}
