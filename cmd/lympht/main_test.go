package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles the current package once per test run, so these
// tests can exercise the real os.Exit() behavior of hook-intercept and
// run the resulting binary as a subprocess — something in-process unit
// tests can't observe.
//
// Built into ./.testbin (relative to this package), NOT t.TempDir(): some
// hardened CI runners mount /tmp noexec, which lets `go build` write the
// binary there but makes exec.Command fail to actually run it. Building
// next to the source keeps it on whatever filesystem `go test` itself
// already runs from, which must be exec-able.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(".", ".testbin"))
	if err != nil {
		t.Fatalf("resolving build dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("creating build dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	bin := filepath.Join(dir, "lympht")
	// -buildvcs=false: this repo's CI runs `go test`/`go build` against a
	// git checkout on a shared Tekton PVC workspace where `git status`
	// fails with a dubious-ownership error (exit 128) — `go build` tries
	// to embed VCS info by default and aborts with "error obtaining VCS
	// status" unless told not to bother. Confirmed via a manual debug
	// PipelineRun capturing real test-run logs (this failure has no
	// archived logs in the normal CI run, and locally it doesn't
	// reproduce — the checkout here isn't on that shared PVC).
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

// TestHookIntercept_BlocksOnFetchFailure guards against
// infra-lympht-silent-substitution-failure-on-403: a Vault/k8s lookup
// failure (e.g. 403 for a path outside the ambient identity's policy) must
// exit 2 (Claude Code's PreToolUse "blocking" contract) so the tool call is
// aborted, instead of exit 1 (non-blocking — Claude Code would silently run
// the ORIGINAL command with the raw {{lympht:...}} placeholder text intact,
// as if it were real data).
func TestHookIntercept_BlocksOnFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	bin := buildBinary(t)
	input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/foo#pass}}"}}`
	cmd := exec.Command(bin, "hook-intercept")
	cmd.Env = append(os.Environ(), "VAULT_ADDR="+srv.URL, "VAULT_TOKEN=dummy")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected the process to exit with an error, got %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	if code := exitErr.ExitCode(); code != 2 {
		t.Errorf("exit code = %d, want 2 (stdout=%q stderr=%q)", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty on failure — no hookResponse (and no placeholder passthrough) — got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "substitution failed") {
		t.Errorf("stderr should explain the failure, got %q", stderr.String())
	}
}

// TestHookIntercept_SubstitutesAndExitsZeroOnSuccess is the success-path
// counterpart, confirming the fix didn't break the normal case.
func TestHookIntercept_SubstitutesAndExitsZeroOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"pass":"secret99"}}}`))
	}))
	defer srv.Close()

	bin := buildBinary(t)
	input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/foo#pass}}"}}`
	cmd := exec.Command(bin, "hook-intercept")
	cmd.Env = append(os.Environ(), "VAULT_ADDR="+srv.URL, "VAULT_TOKEN=dummy")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("expected success, got %v (stderr=%q)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "secret99") {
		t.Errorf("expected substituted value in hookResponse, got %q", stdout.String())
	}
}
