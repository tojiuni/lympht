package hook_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tojiuni/lympht/internal/hook"
	"github.com/tojiuni/lympht/internal/runner"
)

const rtSecret = "s3cretValue99"

func hookOut(t *testing.T, cmd string, fetcher *mockFetcher) string {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": cmd}})
	var out strings.Builder
	if err := hook.RunWithFetcher(strings.NewReader(string(in)), &out, fetcher); err != nil {
		t.Fatalf("hook: %v", err)
	}
	return out.String()
}

// Merge condition 2: the default mode keeps the original behaviour.
func TestDefaultModeUnchanged(t *testing.T) {
	withTempState(t)
	t.Setenv(hook.ModeEnv, "")
	f := &mockFetcher{data: map[string]string{"vault:ns/foo#pass": rtSecret}}
	got := hookOut(t, "echo {{lympht:vault:ns/foo#pass}}", f)
	if !strings.Contains(got, rtSecret) {
		t.Fatalf("default mode must still substitute in the hook: %s", got)
	}
}

// Merge condition 1: in runtime mode the hook stdout (which the harness
// stores as hook_success) never contains the plaintext, and what the wrapper
// executes and prints is masked too.
func TestRuntimeModeNoPlaintextAnywhere(t *testing.T) {
	withTempState(t)
	t.Setenv(hook.ModeEnv, hook.ModeRuntime)
	hook.SelfPath = func() string { return "/usr/local/bin/lympht" }
	f := &mockFetcher{data: map[string]string{"vault:ns/foo#pass": rtSecret}}

	got := hookOut(t, `v={{lympht:vault:ns/foo#pass}}; echo "len=${#v}"; echo "val=$v"`, f)
	if strings.Contains(got, rtSecret) {
		t.Fatalf("plaintext in hook stdout: %s", got)
	}
	var resp struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
			UpdatedInput       struct {
				Command string `json:"command"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(got), &resp); err != nil {
		t.Fatal(err)
	}
	wrapped := resp.HookSpecificOutput.UpdatedInput.Command
	const prefix = "'/usr/local/bin/lympht' run --b64 "
	if resp.HookSpecificOutput.PermissionDecision != "allow" || !strings.HasPrefix(wrapped, prefix) {
		t.Fatalf("unexpected wrapper: %q", wrapped)
	}
	payload := strings.TrimPrefix(wrapped, prefix)
	orig, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || !strings.Contains(string(orig), "{{lympht:vault:ns/foo#pass}}") {
		t.Fatalf("wrapper payload must be the original placeholder command: %v %q", err, orig)
	}

	// Execute what the wrapper would execute: output (what a tool result or a
	// background output file receives) must be masked.
	var out, errb bytes.Buffer
	code := runner.Run(payload, f, runner.Options{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if strings.Contains(out.String(), rtSecret) || strings.Contains(errb.String(), rtSecret) {
		t.Fatalf("plaintext in command output: %q %q", out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "len=13") {
		t.Fatalf("command did not receive the value: %q", out.String())
	}
}

// A failed resolution must still block in the hook, in both modes.
func TestRuntimeModeResolveFailureStillBlocks(t *testing.T) {
	withTempState(t)
	t.Setenv(hook.ModeEnv, hook.ModeRuntime)
	in := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/none#x}}"}}`
	var out strings.Builder
	if err := hook.RunWithFetcher(strings.NewReader(in), &out, &mockFetcher{}); err == nil {
		t.Fatal("expected an error for an unresolvable placeholder")
	}
}

// No placeholder: nothing is wrapped in either mode.
func TestRuntimeModePassthroughWithoutPlaceholder(t *testing.T) {
	withTempState(t)
	t.Setenv(hook.ModeEnv, hook.ModeRuntime)
	if got := hookOut(t, "echo hello", &mockFetcher{}); got != "" {
		t.Fatalf("expected passthrough, got %q", got)
	}
}
