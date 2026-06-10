package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/lympht/internal/hook"
	"github.com/tojiuni/lympht/internal/inject"
	"github.com/tojiuni/lympht/internal/vault"
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
		Short: "PreToolUse hook entry point — reads tool call JSON from stdin",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := vault.NewClient()
			if err != nil {
				return err
			}
			return hook.RunWithFetcher(os.Stdin, os.Stdout, client)
		},
	}
}

func injectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inject -- <command>",
		Short: "Substitute placeholders and print the resolved command (does not execute)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: lympht inject -- <command with placeholders>")
			}
			raw := strings.Join(args, " ")
			client, err := vault.NewClient()
			if err != nil {
				return err
			}
			resolved, err := inject.Substitute(raw, client)
			if err != nil {
				return err
			}
			fmt.Println(resolved)
			return nil
		},
	}
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <vault-path>",
		Short: "List fields at a Vault path (values masked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := vault.NewClient()
			if err != nil {
				return err
			}
			fields, err := client.ListFields(args[0])
			if err != nil {
				return err
			}
			sort.Strings(fields)
			fmt.Printf("Fields at %s:\n", args[0])
			for _, f := range fields {
				fmt.Printf("  ✓ %s\n", f)
			}
			return nil
		},
	}
}
