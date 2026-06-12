# lympht — kubectl Secret Backend Design

**Date:** 2026-06-12  
**Status:** Approved  
**Scope:** Phase 2 — kubectl backend + explicit source prefix in placeholder format

---

## Problem

현재 lympht는 Vault KV v2만 지원한다. Kubernetes Secret(`kubectl get secret`)도 LLM-safe하게 참조할 수 없다.

## Solution

- Placeholder에 명시적 backend 프리픽스(`vault:` / `k8s:`) 추가
- `MultiFetcher`로 라우팅, `kube.Client`로 kubectl backend 구현
- 구형식(`{{lympht:path#field}}`)은 에러 처리 (hard migration)

---

## Placeholder Format

```
{{lympht:vault:<vault-path>#<field>}}
{{lympht:k8s:<namespace>/<secret-name>#<key>}}
```

### 예시

```bash
# Vault (기존 형식에서 마이그레이션)
curl -H "Authorization: Bearer {{lympht:vault:neunexus/someservice#token}}" https://api.example.com

# kubectl Secret
kubectl annotate pod mypod s3-bucket={{lympht:k8s:cogito-svc/cogito-s3#bucket-name}}

# 여러 backend 혼용
docker login registry.example.com \
  -u {{lympht:vault:neunexus/registry#username}} \
  -p {{lympht:k8s:myns/registry-secret#password}}
```

### 구형식 에러

```
{{lympht:neunexus/foo#password}}
→ error: lympht: unknown backend in path "neunexus/foo" — use vault: or k8s: prefix
```

---

## Architecture

```
Claude (placeholder 작성)
    ↓ Bash tool call
PreToolUse hook (settings.json)
    ↓ stdin: tool call JSON
lympht hook-intercept
    ↓ placeholder 파싱 (backend prefix 포함 path 추출)
MultiFetcher.GetField(path, field)
    ├─ "vault:..." → vault.Client → Vault KV v2 API
    └─ "k8s:..."  → kube.Client  → kubectl subprocess → base64 decode
    ↓ 값 치환
Claude Code가 실행 (secret 값은 LLM context에 없음)
```

---

## File Structure

```
internal/
├── vault/client.go         (기존, 무변경)
├── kube/client.go          (신규)
└── inject/
    ├── parser.go           (regex 수정 — backend prefix 포함)
    ├── multi.go            (신규 — MultiFetcher + Lister 인터페이스)
    └── parser_test.go      (업데이트)
cmd/lympht/main.go          (MultiFetcher wiring)
```

---

## MultiFetcher (`internal/inject/multi.go`)

```go
// Fetcher retrieves a secret field. (기존 인터페이스, 무변경)
type Fetcher interface {
    GetField(path, field string) (string, error)
}

// Lister lists available field names at a path (values masked).
type Lister interface {
    ListFields(path string) ([]string, error)
}

// MultiFetcher dispatches to vault or kube backend based on path prefix.
type MultiFetcher struct {
    Vault interface {
        Fetcher
        Lister
    }
    Kube interface {
        Fetcher
        Lister
    }
}

func (m *MultiFetcher) GetField(path, field string) (string, error)
func (m *MultiFetcher) ListFields(path string) ([]string, error)
```

- `vault:` 프리픽스 → Vault backend
- `k8s:` 프리픽스 → kube backend
- 그 외 → `unknown backend` 에러

---

## kube.Client (`internal/kube/client.go`)

**`GetField(path, field string) (string, error)`**
- `path` 형식: `<namespace>/<secret-name>`
- `kubectl get secret <name> -n <namespace> -o json` subprocess 실행
- `.data.<field>` 추출 → base64 디코딩 → 값 반환

**`ListFields(path string) ([]string, error)`**
- 동일 subprocess, `.data` 키 목록만 반환 (값 없음)

**kubeconfig**: 환경의 현재 default context 사용 (`kubectl` 바이너리 그대로 상속)

---

## `check` Command 변경

```bash
lympht check vault:neunexus/github-webhook   # Vault ListFields
lympht check k8s:cogito-svc/cogito-s3        # kubectl .data 키 목록
```

`check` 커맨드가 `MultiFetcher`의 `ListFields`를 호출 → 내부 라우팅으로 처리.

---

## Migration 범위

| 파일 | 변경 내용 |
|------|-----------|
| `internal/inject/parser.go` | regex 수정, 구형식 에러 추가 |
| `internal/inject/multi.go` | 신규 생성 |
| `internal/kube/client.go` | 신규 생성 |
| `internal/inject/parser_test.go` | 테스트 업데이트 |
| `internal/vault/client_test.go` | 무변경 |
| `cmd/lympht/main.go` | MultiFetcher wiring |
| `docs/runbook.md` | 모든 예시 `vault:` 형식으로 업데이트 |
| `docs/superpowers/specs/2026-06-10-*.md` | placeholder 예시 업데이트 |
| `~/.claude/CLAUDE.md` (global) | placeholder 예시 + known paths 업데이트 |

---

## Security Properties

- 기존 보안 속성 모두 유지
- `kubectl` subprocess stdout은 lympht 프로세스 메모리에서만 읽힘
- base64 디코딩된 값은 LLM context에 반환되지 않음
- `lympht check k8s:...`는 키 이름만 반환, 값 없음

---

## Out of Scope

- kubeconfig context 명시적 선택 (`--context` flag) — 필요 시 Phase 3
- AWS Secrets Manager 등 추가 backend — 동일 MultiFetcher 패턴으로 추후 추가 가능
- kubectl 바이너리 PATH 설정 — 환경 기본값 사용
