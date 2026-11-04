package runner

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/tojiuni/lympht/internal/redact"
)

type fake map[string]string

func (f fake) GetField(path, field string) (string, error) {
	v, ok := f[path+"#"+field]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func enc(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func run(t *testing.T, cmd string, f fake, stdin string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(enc(cmd), f, Options{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb})
	return out.String(), errb.String(), code
}

const secret = "s3cretValue99"

func TestUsesValueButMasksOutput(t *testing.T) {
	f := fake{"vault:ns/a#k": secret}
	out, _, code := run(t, `v={{lympht:vault:ns/a#k}}; echo "len=${#v}"; echo "val=$v"`, f, "")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("plaintext in output: %q", out)
	}
	if !strings.Contains(out, "len=13") || !strings.Contains(out, "val="+redact.Marker) {
		t.Fatalf("got %q", out)
	}
}

func TestStderrMasked(t *testing.T) {
	f := fake{"vault:ns/a#k": secret}
	_, errb, _ := run(t, `echo {{lympht:vault:ns/a#k}} >&2`, f, "")
	if strings.Contains(errb, secret) {
		t.Fatalf("plaintext on stderr: %q", errb)
	}
}

func TestExitCodePropagates(t *testing.T) {
	_, _, code := run(t, `exit 7`, fake{}, "")
	if code != 7 {
		t.Fatalf("code %d", code)
	}
}

func TestStdinPreserved(t *testing.T) {
	out, _, _ := run(t, `cat`, fake{}, "piped-input")
	if out != "piped-input" {
		t.Fatalf("got %q", out)
	}
}

func TestResolveFailureBlocks(t *testing.T) {
	out, errb, code := run(t, `echo hi {{lympht:vault:ns/missing#k}}`, fake{}, "")
	if code != 2 || out != "" {
		t.Fatalf("code %d out %q", code, out)
	}
	if !strings.Contains(errb, "ns/missing") {
		t.Fatalf("error should name the path: %q", errb)
	}
}

func TestLargeScript(t *testing.T) {
	f := fake{"vault:ns/a#k": secret}
	big := strings.Repeat("# padding line to exceed the pipe buffer\n", 4000)
	out, _, code := run(t, big+`echo ok {{lympht:vault:ns/a#k}}`, f, "")
	if code != 0 || strings.Contains(out, secret) || !strings.Contains(out, "ok") {
		t.Fatalf("code %d out %q", code, out)
	}
}

// The resolved value must not be visible to other processes through argv.
func TestValueNotInChildArgv(t *testing.T) {
	f := fake{"vault:ns/a#k": secret}
	out, _, code := run(t, `tr '\0' ' ' < /proc/$$/cmdline; echo; echo {{lympht:vault:ns/a#k}} >/dev/null`, f, "")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("value leaked into cmdline: %q", out)
	}
	if !strings.Contains(out, "/dev/fd/3") {
		t.Fatalf("expected script to be read from fd 3: %q", out)
	}
}
