package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/lympht/internal/hook"
	"github.com/tojiuni/lympht/internal/inject"
	"github.com/tojiuni/lympht/internal/kube"
	"github.com/tojiuni/lympht/internal/vault"
)

func main() {
	root := &cobra.Command{
		Use:   "lympht",
		Short: "LLM-safe secret injector for Claude Code",
	}
	root.AddCommand(hookInterceptCmd())
	root.AddCommand(injectCmd())
	root.AddCommand(checkCmd())
	root.AddCommand(installCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newMultiFetcher() (*inject.MultiFetcher, error) {
	vaultClient, err := vault.NewClient()
	if err != nil {
		return nil, err
	}
	return &inject.MultiFetcher{
		Vault: vaultClient,
		Kube:  kube.NewClient(),
	}, nil
}

func hookInterceptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hook-intercept",
		Short: "PreToolUse hook entry point — reads tool call JSON from stdin",
		RunE: func(cmd *cobra.Command, args []string) error {
			multi, err := newMultiFetcher()
			if err != nil {
				// exit(2), not a plain returned error: Claude Code's PreToolUse
				// hook contract treats exit code 2 as BLOCKING (the tool call
				// is aborted) and any other non-zero exit as non-blocking
				// (stderr is shown, but the ORIGINAL, unsubstituted command
				// still runs). Returning this error up to root.Execute() would
				// hit main()'s generic os.Exit(1) below, which silently lets
				// the raw {{lympht:...}} placeholder text through as if it
				// were real data (infra-lympht-silent-substitution-failure-on-403)
				// — e.g. a 403 from a vault path outside the ambient
				// identity's policy scope must never look like a successful
				// substitution. inject/check commands are unaffected: their
				// errors still flow through the normal RunE -> Execute()
				// path and exit 1, which has no special meaning to them.
				fmt.Fprintln(os.Stderr, "lympht hook-intercept: fetcher init failed:", err)
				os.Exit(2)
			}
			if err := hook.RunWithFetcher(os.Stdin, os.Stdout, multi); err != nil {
				fmt.Fprintln(os.Stderr, "lympht hook-intercept: blocked:", err)
				os.Exit(2)
			}
			return nil
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
			multi, err := newMultiFetcher()
			if err != nil {
				return err
			}
			resolved, err := inject.Substitute(raw, multi)
			if err != nil {
				return err
			}
			fmt.Println(resolved)
			return nil
		},
	}
}

// installCmd copies this running binary to dest, atomically (write to
// dest+".tmp" in the same directory, then rename — rename is atomic within
// a filesystem, so a reader never observes a partially-written file, and a
// process already running the old inode keeps working until it exits).
//
// This exists because the lympht image is FROM scratch (no shell, no `cp`)
// — the standard "shell initContainer copies a binary onto a shared PVC"
// pattern doesn't work here. Running `lympht install <dest>` as the
// initContainer's command needs nothing but the binary itself.
func installCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <dest>",
		Short: "Copy this binary to dest (atomic rename). For scratch-image initContainers with no shell/cp.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := args[0]
			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolving own executable path: %w", err)
			}
			src, err := os.Open(self)
			if err != nil {
				return fmt.Errorf("opening %s: %w", self, err)
			}
			defer src.Close()

			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
			}
			tmp := dest + ".tmp"
			out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				return fmt.Errorf("creating %s: %w", tmp, err)
			}
			if _, err := io.Copy(out, src); err != nil {
				out.Close()
				os.Remove(tmp)
				return fmt.Errorf("copying to %s: %w", tmp, err)
			}
			if err := out.Close(); err != nil {
				os.Remove(tmp)
				return fmt.Errorf("closing %s: %w", tmp, err)
			}
			if err := os.Rename(tmp, dest); err != nil {
				os.Remove(tmp)
				return fmt.Errorf("renaming %s to %s: %w", tmp, dest, err)
			}
			fmt.Println("OK: installed to", dest)
			return nil
		},
	}
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <path>",
		Short: "List fields at a secret path (values masked). Use vault: or k8s: prefix.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			multi, err := newMultiFetcher()
			if err != nil {
				return err
			}
			fields, err := multi.ListFields(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Fields at %s:\n", args[0])
			for _, f := range fields {
				fmt.Printf("  ✓ %s\n", f)
			}
			return nil
		},
	}
}
