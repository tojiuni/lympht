package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "lympht",
		Short: "LLM-safe Vault secret injector for Claude Code",
	}

	root.AddCommand(hookInterceptCmd())
	root.AddCommand(injectCmd())
	root.AddCommand(checkCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func hookInterceptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hook-intercept",
		Short: "PreToolUse hook entry point (reads tool call JSON from stdin)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
}

func injectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inject -- <command>",
		Short: "Substitute placeholders and run command directly",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <vault-path>",
		Short: "List fields at a Vault path (values masked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented")
		},
	}
}
