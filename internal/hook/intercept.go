package hook

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/tojiuni/lympht/internal/guard"
	"github.com/tojiuni/lympht/internal/inject"
	"github.com/tojiuni/lympht/internal/scope"
)

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

	substituted, err := inject.Substitute(call.ToolInput.Command, fetcher)
	if err != nil {
		return err
	}

	var resp hookResponse
	resp.HookSpecificOutput.HookEventName = "PreToolUse"
	resp.HookSpecificOutput.PermissionDecision = "allow"
	resp.HookSpecificOutput.UpdatedInput.Command = substituted
	return json.NewEncoder(w).Encode(resp)
}
