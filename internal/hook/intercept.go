package hook

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/tojiuni/lympht/internal/guard"
	"github.com/tojiuni/lympht/internal/inject"
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
// Every Bash command is checked against guard.CheckCommand first —
// independent of whether it contains any lympht placeholder — and a match
// there returns an error (which the caller turns into a blocking exit(2),
// see cmd/lympht/main.go's hookInterceptCmd). Otherwise, if the command
// contains lympht placeholders, substitutes them using fetcher and writes
// the modify response JSON to w. If no placeholders or non-Bash tool, writes
// nothing (passthrough, exit 0).
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

	if err := guard.CheckCommand(call.ToolInput.Command); err != nil {
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
