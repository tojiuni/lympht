# kubectl Secret Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add kubectl secret backend to lympht with explicit `vault:`/`k8s:` placeholder prefixes, and hard-migrate all existing docs/examples.

**Architecture:** `MultiFetcher` wraps both `vault.Client` and `kube.Client` behind the `inject.Backend` interface, routing by placeholder prefix (`vault:` → Vault KV v2, `k8s:` → kubectl subprocess). The existing `inject.Substitute` and `hook.RunWithFetcher` are unchanged.

**Tech Stack:** Go 1.21+, cobra, standard `os/exec` (kubectl subprocess), base64 decode via `encoding/base64`.

---

## File Map

| Action | File |
|--------|------|
| Create | `internal/inject/multi.go` |
| Create | `internal/inject/multi_test.go` |
| Create | `internal/kube/client.go` |
| Create | `internal/kube/client_test.go` |
| Modify | `internal/inject/parser_test.go` |
| Modify | `internal/hook/intercept_test.go` |
| Modify | `cmd/lympht/main.go` |
| Modify | `docs/runbook.md` |
| Modify | `docs/superpowers/specs/2026-06-10-lympht-llm-proxy-design.md` |
| Modify | `~/.claude/CLAUDE.md` |

---

### Task 1: `MultiFetcher` + `Lister` interface

**Files:**
- Create: `internal/inject/multi.go`
- Create: `internal/inject/multi_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/inject/multi_test.go`:

```go
package inject_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tojiuni/lympht/internal/inject"
)

type stubBackend struct {
	data map[string]string // "path#field" → value
}

func (s *stubBackend) GetField(path, field string) (string, error) {
	val, ok := s.data[path+"#"+field]
	if !ok {
		return "", fmt.Errorf("not found: %s#%s", path, field)
	}
	return val, nil
}

func (s *stubBackend) ListFields(path string) ([]string, error) {
	prefix := path + "#"
	var fields []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			fields = append(fields, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(fields)
	return fields, nil
}

func TestMultiFetcher_GetField_Vault(t *testing.T) {
	vaultB := &stubBackend{data: map[string]string{"neunexus/foo#password": "secret"}}
	multi := &inject.MultiFetcher{Vault: vaultB, Kube: &stubBackend{}}

	val, err := multi.GetField("vault:neunexus/foo", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "secret" {
		t.Errorf("got %q, want %q", val, "secret")
	}
}

func TestMultiFetcher_GetField_Kube(t *testing.T) {
	kubeB := &stubBackend{data: map[string]string{"cogito-svc/cogito-s3#access-key": "AKID"}}
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: kubeB}

	val, err := multi.GetField("k8s:cogito-svc/cogito-s3", "access-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "AKID" {
		t.Errorf("got %q, want %q", val, "AKID")
	}
}

func TestMultiFetcher_GetField_UnknownBackend(t *testing.T) {
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: &stubBackend{}}

	_, err := multi.GetField("neunexus/foo", "password") // missing prefix
	if err == nil {
		t.Fatal("expected error for path without backend prefix")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("error %q should contain 'unknown backend'", err.Error())
	}
}

func TestMultiFetcher_ListFields_Vault(t *testing.T) {
	vaultB := &stubBackend{data: map[string]string{
		"neunexus/foo#password": "s",
		"neunexus/foo#token":    "t",
	}}
	multi := &inject.MultiFetcher{Vault: vaultB, Kube: &stubBackend{}}

	fields, err := multi.ListFields("vault:neunexus/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"password", "token"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("got %v, want %v", fields, want)
	}
}

func TestMultiFetcher_ListFields_Kube(t *testing.T) {
	kubeB := &stubBackend{data: map[string]string{
		"myns/mysecret#key1": "v",
		"myns/mysecret#key2": "v",
	}}
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: kubeB}

	fields, err := multi.ListFields("k8s:myns/mysecret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"key1", "key2"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("got %v, want %v", fields, want)
	}
}

func TestMultiFetcher_ListFields_UnknownBackend(t *testing.T) {
	multi := &inject.MultiFetcher{Vault: &stubBackend{}, Kube: &stubBackend{}}

	_, err := multi.ListFields("neunexus/foo") // missing prefix
	if err == nil {
		t.Fatal("expected error for path without backend prefix")
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go test ./internal/inject/... -run TestMultiFetcher -v
```

Expected: `cannot find package` or `undefined: inject.MultiFetcher`

- [ ] **Step 3: Implement `internal/inject/multi.go`**

```go
package inject

import (
	"fmt"
	"sort"
	"strings"
)

// Lister lists available field names at a path without revealing values.
type Lister interface {
	ListFields(path string) ([]string, error)
}

// Backend is a secret backend that can fetch fields and list keys.
type Backend interface {
	Fetcher
	Lister
}

// MultiFetcher routes GetField and ListFields to vault or kube backend
// based on the path prefix ("vault:" or "k8s:").
type MultiFetcher struct {
	Vault Backend
	Kube  Backend
}

// GetField implements Fetcher.
func (m *MultiFetcher) GetField(path, field string) (string, error) {
	backend, stripped, err := m.resolve(path)
	if err != nil {
		return "", err
	}
	return backend.GetField(stripped, field)
}

// ListFields implements Lister. Returns sorted field names.
func (m *MultiFetcher) ListFields(path string) ([]string, error) {
	backend, stripped, err := m.resolve(path)
	if err != nil {
		return nil, err
	}
	fields, err := backend.ListFields(stripped)
	if err != nil {
		return nil, err
	}
	sort.Strings(fields)
	return fields, nil
}

func (m *MultiFetcher) resolve(path string) (Backend, string, error) {
	switch {
	case strings.HasPrefix(path, "vault:"):
		return m.Vault, strings.TrimPrefix(path, "vault:"), nil
	case strings.HasPrefix(path, "k8s:"):
		return m.Kube, strings.TrimPrefix(path, "k8s:"), nil
	default:
		return nil, "", fmt.Errorf("lympht: unknown backend in path %q — use vault: or k8s: prefix", path)
	}
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```bash
go test ./internal/inject/... -run TestMultiFetcher -v
```

Expected: all `TestMultiFetcher_*` PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
git add internal/inject/multi.go internal/inject/multi_test.go
git commit -m "feat(inject): MultiFetcher + Lister interface for vault/k8s routing"
```

---

### Task 2: `kube.Client`

**Files:**
- Create: `internal/kube/client.go`
- Create: `internal/kube/client_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/kube/client_test.go`:

```go
package kube_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/tojiuni/lympht/internal/kube"
)

func fakeRun(payload string) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		return []byte(payload), nil
	}
}

func encoded(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestGetField_ReturnsDecodedValue(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"access-key":%q,"secret-key":%q}}`,
		encoded("AKID123"), encoded("superSecret"))

	client := kube.NewClientWithRunner(fakeRun(payload))

	val, err := client.GetField("cogito-svc/cogito-s3", "access-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "AKID123" {
		t.Errorf("got %q, want %q", val, "AKID123")
	}
}

func TestGetField_MissingFieldReturnsError(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"other-key":%q}}`, encoded("val"))
	client := kube.NewClientWithRunner(fakeRun(payload))

	_, err := client.GetField("ns/secret", "missing-field")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestGetField_InvalidPathReturnsError(t *testing.T) {
	client := kube.NewClientWithRunner(fakeRun(`{}`))

	_, err := client.GetField("no-slash", "key") // missing namespace/name separator
	if err == nil {
		t.Fatal("expected error for malformed path")
	}
}

func TestGetField_KubectlErrorReturnsError(t *testing.T) {
	client := kube.NewClientWithRunner(func(string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("exit status 1")
	})

	_, err := client.GetField("ns/secret", "key")
	if err == nil {
		t.Fatal("expected error when kubectl fails")
	}
}

func TestListFields_ReturnsKeys(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"key1":%q,"key2":%q}}`, encoded("a"), encoded("b"))
	client := kube.NewClientWithRunner(fakeRun(payload))

	fields, err := client.ListFields("ns/mysecret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 {
		t.Errorf("got %d fields, want 2: %v", len(fields), fields)
	}
}

func TestGetField_KubectlArgsCorrect(t *testing.T) {
	payload := fmt.Sprintf(`{"data":{"k":%q}}`, encoded("v"))
	var gotArgs []string
	client := kube.NewClientWithRunner(func(name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte(payload), nil
	})

	_, err := client.GetField("mynamespace/mysecret", "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"kubectl", "get", "secret", "mysecret", "-n", "mynamespace", "-o", "json"}
	for i, w := range want {
		if i >= len(gotArgs) || gotArgs[i] != w {
			t.Errorf("args[%d] = %q, want %q (full: %v)", i, gotArgs[i], w, gotArgs)
		}
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go test ./internal/kube/... -v
```

Expected: `cannot find package "github.com/tojiuni/lympht/internal/kube"`

- [ ] **Step 3: Implement `internal/kube/client.go`**

```go
package kube

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client fetches Kubernetes secrets via kubectl.
type Client struct {
	run func(name string, args ...string) ([]byte, error)
}

// NewClient returns a Client using the system kubectl and current kubeconfig context.
func NewClient() *Client {
	return &Client{run: func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}}
}

// NewClientWithRunner returns a Client using the given command runner.
// For testing only.
func NewClientWithRunner(run func(string, ...string) ([]byte, error)) *Client {
	return &Client{run: run}
}

type secretJSON struct {
	Data map[string]string `json:"data"`
}

// GetField fetches and base64-decodes a key from a Kubernetes secret.
// path format: "<namespace>/<secret-name>"
func (c *Client) GetField(path, field string) (string, error) {
	data, err := c.fetchData(path)
	if err != nil {
		return "", err
	}
	encoded, ok := data[field]
	if !ok {
		return "", fmt.Errorf("field %q not found in secret %q", field, path)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode %q in secret %q: %w", field, path, err)
	}
	return string(decoded), nil
}

// ListFields returns all key names in a Kubernetes secret without revealing values.
// path format: "<namespace>/<secret-name>"
func (c *Client) ListFields(path string) ([]string, error) {
	data, err := c.fetchData(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (c *Client) fetchData(path string) (map[string]string, error) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("kube: path must be <namespace>/<secret-name>, got %q", path)
	}
	namespace, name := parts[0], parts[1]

	out, err := c.run("kubectl", "get", "secret", name, "-n", namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("kubectl get secret %s -n %s: %w", name, namespace, err)
	}

	var s secretJSON
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("parsing kubectl output for %q: %w", path, err)
	}
	if s.Data == nil {
		return nil, fmt.Errorf("secret %q has no data", path)
	}
	return s.Data, nil
}
```

- [ ] **Step 4: Verify `kube.Client` satisfies `inject.Backend`**

Add this compile-time check to `internal/kube/client.go` (after imports, before `Client` struct):

```go
// verify at compile time
var _ interface {
	GetField(string, string) (string, error)
	ListFields(string) ([]string, error)
} = (*Client)(nil)
```

- [ ] **Step 5: Run tests — confirm they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go test ./internal/kube/... -v
```

Expected: all `Test*` PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
git add internal/kube/
git commit -m "feat(kube): kubectl secret backend with base64 decode"
```

---

### Task 3: Update `parser_test.go` for new placeholder format

**Files:**
- Modify: `internal/inject/parser_test.go`

- [ ] **Step 1: Update mock data keys and input strings**

In `internal/inject/parser_test.go`, apply these changes:

1. In `TestSubstitute`, change the `fetcher` mock data from:
```go
fetcher := &mockFetcher{
    data: map[string]string{
        "neunexus/foo#password": "secret123",
        "neunexus/bar#api-key":  "key456",
    },
}
```
to:
```go
fetcher := &mockFetcher{
    data: map[string]string{
        "vault:neunexus/foo#password": "secret123",
        "vault:neunexus/bar#api-key":  "key456",
    },
}
```

2. In `TestSubstitute` test cases, replace all `{{lympht:neunexus/...}}` with `{{lympht:vault:neunexus/...}}`:

| Old input | New input |
|-----------|-----------|
| `{{lympht:neunexus/foo#password}}` | `{{lympht:vault:neunexus/foo#password}}` |
| `{{lympht:neunexus/bar#api-key}}` | `{{lympht:vault:neunexus/bar#api-key}}` |

Update `want` strings for the `double-quoted placeholder` case:
- old: `vault kv put secret/x pass='secret123'`  
- new: `vault kv put secret/x pass='secret123'` (same, just the input changes)

3. In `TestHasPlaceholders`, update the `true` case:
```go
{"echo {{lympht:vault:ns/path#key}}", true},
```

4. In the `single quote in value` subtest, change:
```go
f := &mockFetcher{data: map[string]string{"p#k": "it's"}}
got, err := inject.Substitute(`PGPASSWORD={{lympht:p#k}}`, f)
```
to:
```go
f := &mockFetcher{data: map[string]string{"vault:p#k": "it's"}}
got, err := inject.Substitute(`PGPASSWORD={{lympht:vault:p#k}}`, f)
```

5. In `TestSubstituteSpecialCharPasswords`, change:
```go
f := &mockFetcher{data: map[string]string{"p#pw": c.password}}
got, err := inject.Substitute(`PGPASSWORD={{lympht:p#pw}} psql`, f)
```
to:
```go
f := &mockFetcher{data: map[string]string{"vault:p#pw": c.password}}
got, err := inject.Substitute(`PGPASSWORD={{lympht:vault:p#pw}} psql`, f)
```

- [ ] **Step 2: Run tests — confirm they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go test ./internal/inject/... -v
```

Expected: all tests PASS (mock fetcher accepts any path string, no routing logic in parser)

- [ ] **Step 3: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
git add internal/inject/parser_test.go
git commit -m "test(inject): update parser tests to vault: prefix format"
```

---

### Task 4: Update `hook/intercept_test.go` for new format

**Files:**
- Modify: `internal/hook/intercept_test.go`

- [ ] **Step 1: Update placeholder strings and mock data**

In `internal/hook/intercept_test.go`:

1. `TestRun_SubstitutesPlaceholder`: change input and mock data:
```go
// Before:
input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:ns/foo#pass}}"}}`
fetcher := &mockFetcher{data: map[string]string{"ns/foo#pass": "secret99"}}

// After:
input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/foo#pass}}"}}`
fetcher := &mockFetcher{data: map[string]string{"vault:ns/foo#pass": "secret99"}}
```

2. `TestRun_VaultErrorReturnsError`: change input:
```go
// Before:
input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:ns/missing#pass}}"}}`

// After:
input := `{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:ns/missing#pass}}"}}`
```

- [ ] **Step 2: Run tests — confirm they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go test ./internal/hook/... -v
```

Expected: all tests PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
git add internal/hook/intercept_test.go
git commit -m "test(hook): update intercept tests to vault: prefix format"
```

---

### Task 5: Wire `MultiFetcher` in `cmd/lympht/main.go`

**Files:**
- Modify: `cmd/lympht/main.go`

- [ ] **Step 1: Replace `main.go` with MultiFetcher wiring**

Replace the full content of `cmd/lympht/main.go`:

```go
package main

import (
	"fmt"
	"os"
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
				return err
			}
			return hook.RunWithFetcher(os.Stdin, os.Stdout, multi)
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
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go build ./...
```

Expected: exits 0, no output

- [ ] **Step 3: Run full test suite**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
go test ./... -v
```

Expected: all tests PASS

- [ ] **Step 4: Smoke test — old prefix rejected**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
echo '{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:neunexus/foo#pass}}"}}' \
  | ./lympht hook-intercept
```

Expected: non-zero exit, stderr contains `unknown backend in path "neunexus/foo"`

- [ ] **Step 5: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
git add cmd/lympht/main.go
git commit -m "feat(main): wire MultiFetcher; route vault:/k8s: placeholder prefixes"
```

---

### Task 6: Migrate docs

**Files:**
- Modify: `docs/runbook.md`
- Modify: `docs/superpowers/specs/2026-06-10-lympht-llm-proxy-design.md`
- Modify: `~/.claude/CLAUDE.md`

- [ ] **Step 1: Update `docs/runbook.md`**

Find every occurrence of `{{lympht:` that does NOT already have `vault:` or `k8s:` after it. Replace with `{{lympht:vault:`. Full list of substitutions:

| Old | New |
|-----|-----|
| `{{lympht:neunexus/github-webhook#secret}}` | `{{lympht:vault:neunexus/github-webhook#secret}}` |
| `{{lympht:neunexus/foo#password}}` | `{{lympht:vault:neunexus/foo#password}}` |
| `{{lympht:neunexus/someservice#token}}` | `{{lympht:vault:neunexus/someservice#token}}` |
| `{{lympht:neunexus/registry#username}}` | `{{lympht:vault:neunexus/registry#username}}` |
| `{{lympht:neunexus/registry#password}}` | `{{lympht:vault:neunexus/registry#password}}` |
| `{{lympht:neunexus/cloudbro/postgres-gopedia#password}}` | `{{lympht:vault:neunexus/cloudbro/postgres-gopedia#password}}` |

Also update the `lympht check` examples:

| Old | New |
|-----|-----|
| `lympht check neunexus/github-webhook` | `lympht check vault:neunexus/github-webhook` |

Add kubectl example section after the Vault examples:

```markdown
### kubectl Secret 예시

```bash
# K8s secret 필드 조회
kubectl annotate pod mypod key={{lympht:k8s:cogito-svc/cogito-s3#bucket-name}}

# 여러 backend 혼용
docker login registry.example.com \
  -u {{lympht:vault:neunexus/registry#username}} \
  -p {{lympht:k8s:myns/registry-creds#password}}
```

```bash
# kubectl secret 키 목록 확인 (값 없음)
lympht check k8s:cogito-svc/cogito-s3
# Fields at k8s:cogito-svc/cogito-s3:
#   ✓ access-key
#   ✓ bucket-name
#   ✓ secret-key
```
```

- [ ] **Step 2: Update `docs/superpowers/specs/2026-06-10-lympht-llm-proxy-design.md`**

In the Placeholder Format section, update examples:
```bash
# Before:
vault kv put secret/foo pass="{{lympht:neunexus/foo#pass}}"

# After:
vault kv put secret/foo pass="{{lympht:vault:neunexus/foo#pass}}"
```

Add a note at the top: `> **Note:** This spec describes Phase 1. Phase 2 (kubectl backend) is documented in \`2026-06-12-kubectl-backend-design.md\`.`

- [ ] **Step 3: Update `~/.claude/CLAUDE.md`**

In the `## lympht — LLM-safe Vault secret proxy` section, update:

1. The usage pattern example:
```bash
# Before:
curl -H "Authorization: Bearer {{lympht:neunexus/someservice#token}}" https://api.example.com

# After:
curl -H "Authorization: Bearer {{lympht:vault:neunexus/someservice#token}}" https://api.example.com
# kubectl secret:
kubectl annotate pod mypod key={{lympht:k8s:<namespace>/<secret-name>#<field>}}
```

2. The `lympht check` example:
```bash
# Before:
lympht check neunexus/github-webhook

# After:
lympht check vault:neunexus/github-webhook
lympht check k8s:<namespace>/<secret-name>
```

3. The placeholder format description:
```
# Before:
{{lympht:path#field}}

# After:
{{lympht:vault:<vault-path>#<field>}}    ← Vault KV v2
{{lympht:k8s:<ns>/<secret-name>#<key>}} ← kubectl secret (auto base64-decoded)
```

- [ ] **Step 4: Verify all old-format occurrences are gone**

```bash
grep -rn '{{lympht:[^v][^a]' \
  /Users/dong-hoshin/Documents/dev/lympht/docs/ \
  ~/.claude/CLAUDE.md
```

Expected: no output (no old-format placeholders remaining)

- [ ] **Step 5: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/lympht
git add docs/runbook.md \
        docs/superpowers/specs/2026-06-10-lympht-llm-proxy-design.md
git commit -m "docs: migrate all placeholders to vault:/k8s: prefix format"

git add ~/.claude/CLAUDE.md
git commit -m "docs(global): update lympht placeholder format to vault:/k8s: prefix"
```

---

## Self-Review

**Spec coverage check:**
- ✅ `vault:` / `k8s:` explicit prefix format — Task 1 (MultiFetcher), Task 3/4 (test migration)
- ✅ Old format → error — `MultiFetcher.resolve()` default case in Task 1
- ✅ kubectl subprocess with base64 decode — Task 2 (`kube.Client`)
- ✅ `lympht check` for both backends — Task 5 (`checkCmd` uses `multi.ListFields`)
- ✅ Hard migration of all docs — Task 6
- ✅ `~/.claude/CLAUDE.md` update — Task 6, Step 3

**Placeholder scan:** No TBDs, no "similar to Task N", all code blocks present.

**Type consistency:**
- `inject.Backend` interface: `GetField(string,string)(string,error)` + `ListFields(string)([]string,error)`
- `vault.Client` — existing methods match signature ✅
- `kube.Client` — Task 2 implements same signatures ✅
- `MultiFetcher.Vault` / `.Kube` both typed as `inject.Backend` ✅
- `multi.ListFields` called in `checkCmd` — `MultiFetcher` has `ListFields` ✅
