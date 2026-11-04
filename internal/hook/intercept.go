package hook

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tojiuni/lympht/internal/guard"
	"github.com/tojiuni/lympht/internal/inject"
	"github.com/tojiuni/lympht/internal/scope"
)

// ModeEnv selects how placeholders are resolved. Unset (default) keeps the
// original behaviour: the hook substitutes the value into the command.
// ModeRuntime defers substitution to `lympht run`.
const (
	ModeEnv     = "LYMPHT_HOOK_MODE"
	ModeRuntime = "runtime"
)

// SelfPath is the lympht binary the wrapper invokes; tests override it.
var SelfPath = func() string {
	p, err := os.Executable()
	if err != nil {
		return "lympht"
	}
	return p
}

func wrapper(original string) string {
	q := "'" + strings.ReplaceAll(SelfPath(), "'", `'\''`) + "'"
	return q + " run --b64 " + base64.StdEncoding.EncodeToString([]byte(original))
}

type toolCall struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type hookSpecificOutput struct {
	HookEventName      string `json:"hookEventName"`
	PermissionDecision string `json:"permissionDecision"`
	UpdatedInput       struct {
		Command string `json:"command"`
	} `json:"updatedInput"`
}

type hookResponse struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

// RunWithFetcher reads a Claude Code PreToolUse JSON payload from r.
// Every Bash command is checked, in order:
//  1. scope.RecordIfProviderRequested — a side effect, never blocks: if this
//     command is a tokenhub credential fetch, remember that provider as
//     active for its TTL window.
//  2. guard.CheckCommand — always-on secret-exposure pattern blocks,
//     independent of any active provider (see package guard's doc comment
//     for why these can never be "unlocked" by an elevation).
//  3. scope.CheckCommand — provider-scoped gating of specific high-privilege
//     kubectl actions (exec, secret reads, node/argocd/tekton mutations)
//     against the currently active provider set.
//
// Any of 2/3 returning an error returns that error (the caller turns it into
// a blocking exit(2), see cmd/lympht/main.go's hookInterceptCmd). Otherwise,
// if the command contains lympht placeholders, substitutes them using
// fetcher and writes the modify response JSON to w. If no placeholders or
// non-Bash tool, writes nothing (passthrough, exit 0).
func RunWithFetcher(r io.Reader, w io.Writer, fetcher inject.Fetcher) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var call toolCall
	if err := json.Unmarshal(data, &call); err != nil {
		return fmt.Errorf("parsing tool call JSON: %w", err)
	}

	if call.ToolName != "Bash" {
		return nil // passthrough: write nothing
	}

	scope.RecordIfProviderRequested(call.ToolInput.Command)

	if err := guard.CheckCommand(call.ToolInput.Command); err != nil {
		return err
	}

	if err := scope.CheckCommand(call.ToolInput.Command); err != nil {
		return err
	}

	if !inject.HasPlaceholders(call.ToolInput.Command) {
		return nil // passthrough: write nothing
	}

	// Resolve once either way: in runtime mode this only validates (a 403 or
	// missing field still blocks here, as before) and the value is dropped.
	substituted, err := inject.Substitute(call.ToolInput.Command, fetcher)
	if err != nil {
		return err
	}
	if os.Getenv(ModeEnv) == ModeRuntime {
		// The harness stores the hook's stdout in the session transcript, so
		// the resolved command must not appear in it. Emit a wrapper that
		// carries only the placeholder text; `lympht run` resolves it at
		// execution time (spec NEU-SPC-DRAFT-20261104).
		substituted = wrapper(call.ToolInput.Command)
	}

	var resp hookResponse
	resp.HookSpecificOutput.HookEventName = "PreToolUse"
	resp.HookSpecificOutput.PermissionDecision = "allow"
	resp.HookSpecificOutput.UpdatedInput.Command = substituted
	return json.NewEncoder(w).Encode(resp)
}
