// Package runner executes a command whose {{lympht:...}} placeholders are
// resolved only here, at execution time (spec NEU-SPC-DRAFT-20261104).
//
// The resolved script is handed to bash through an anonymous pipe
// (/dev/fd/3), never through argv or the environment, so it is absent from
// /proc/<pid>/cmdline and from the hook output the harness records. Output
// of the child is passed through a masking writer.
package runner

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/tojiuni/lympht/internal/inject"
	"github.com/tojiuni/lympht/internal/redact"
)

// recorder remembers every value it hands out so the output can be masked.
type recorder struct {
	inner  inject.Fetcher
	mu     sync.Mutex
	values []string
}

func (r *recorder) GetField(path, field string) (string, error) {
	v, err := r.inner.GetField(path, field)
	if err == nil {
		r.mu.Lock()
		r.values = append(r.values, v)
		r.mu.Unlock()
	}
	return v, err
}

// Options configures Run. Shell defaults to "bash".
type Options struct {
	Shell  string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run decodes the base64 command, resolves its placeholders with fetcher,
// runs it and returns the exit code. A resolution failure returns 2 after
// printing the error (path names only, never values).
func Run(b64 string, fetcher inject.Fetcher, o Options) int {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		fmt.Fprintln(o.Stderr, "lympht run: invalid --b64 payload")
		return 2
	}
	rec := &recorder{inner: fetcher}
	script, err := inject.Substitute(string(raw), rec)
	if err != nil {
		fmt.Fprintln(o.Stderr, "lympht run: blocked:", err)
		return 2
	}

	shell := o.Shell
	if shell == "" {
		shell = "bash"
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(o.Stderr, "lympht run: pipe:", err)
		return 2
	}
	variants := redact.Variants(rec.values)
	outW := redact.NewWriter(o.Stdout, variants)
	errW := redact.NewWriter(o.Stderr, variants)

	cmd := exec.Command(shell, "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{pr}
	cmd.Stdin = o.Stdin
	cmd.Stdout = outW
	cmd.Stderr = errW
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		fmt.Fprintln(o.Stderr, "lympht run: start:", err)
		return 2
	}
	pr.Close()
	go func() {
		// A short script fits the pipe buffer; a longer one is written
		// while bash reads it. Errors (bash exited early) are expected.
		_, _ = pw.Write([]byte(script))
		pw.Close()
	}()
	werr := cmd.Wait()
	_ = outW.Close()
	_ = errW.Close()

	var ee *exec.ExitError
	switch {
	case werr == nil:
		return 0
	case errors.As(werr, &ee):
		return ee.ExitCode()
	default:
		fmt.Fprintln(o.Stderr, "lympht run: wait:", werr)
		return 2
	}
}
