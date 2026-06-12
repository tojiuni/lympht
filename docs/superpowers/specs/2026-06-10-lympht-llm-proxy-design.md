# lympht — LLM Proxy MVP Design

**Date:** 2026-06-10  
**Status:** Approved  
**Scope:** Phase 1 — LLM-safe Vault secret injection via Claude Code PreToolUse hook

> **Note:** Phase 1 spec. Phase 2 (kubectl backend + explicit prefixes) documented in `2026-06-12-kubectl-backend-design.md`.

---

## Problem

LLM(Claude Code)이 Bash 명령을 실행할 때 Vault secret 값이 tool call 출력에 노출된다.
LLM 컨텍스트에 secret 값이 포함되면 보안 경계가 무너진다.

## Solution

Claude가 명령에 `{{lympht:vault:<path>#<field>}}` placeholder를 사용하면,
Claude Code PreToolUse hook이 실행 전 Vault에서 값을 fetch해 치환한다.
LLM은 placeholder만 보고 실제 값은 보지 못한다.

---

## Architecture

```
Claude (placeholder 작성)
    ↓ Bash tool call
PreToolUse hook (settings.json)
    ↓ stdin: tool call JSON
lympht hook-intercept
    ↓ placeholder 파싱
Vault KV v2 API (VAULT_ADDR + VAULT_TOKEN)
    ↓ 값 치환
os/exec → 실행
    ↓ stdout/stderr
Claude (결과만 수신, secret 값 없음)
```

---

## Placeholder Format

```
{{lympht:vault:<vault-path>#<field>}}
```

- `vault-path`: Vault KV v2 경로 (mount prefix 제외, e.g. `neunexus/cloudbro/postgres-gopedia`)
- `field`: secret map의 키 이름 (e.g. `password`, `api-key`)

### 예시

```bash
# Claude가 작성하는 명령
vault kv put secret/foo pass="{{lympht:vault:neunexus/foo#pass}}"

# lympht가 실제 실행하는 명령 (LLM에 안 보임)
vault kv put secret/foo pass="actualvalue123"
```

---

## CLI Commands

| 명령 | 설명 |
|------|------|
| `lympht hook-intercept` | PreToolUse hook entry point. stdin으로 tool JSON 수신, placeholder 치환 후 실행 |
| `lympht inject -- <cmd>` | 직접 실행용. placeholder가 포함된 명령을 치환 후 실행 |
| `lympht check <path>` | Vault 경로의 key 목록과 값 길이만 출력 (값 노출 없음) |

---

## Hook Protocol (Claude Code PreToolUse)

Claude Code는 Bash tool call 시 hook을 실행한다.

**stdin (JSON):**
```json
{
  "tool_name": "Bash",
  "tool_input": {
    "command": "vault kv put ... pass=\"{{lympht:vault:neunexus/foo#pass}}\""
  }
}
```

**hook 동작:**
1. `tool_input.command`에서 `{{lympht:...}}` 패턴 탐색
2. placeholder 없으면 → exit 0 (통과)
3. placeholder 있으면 → Vault fetch, 치환, `os/exec`로 실행
4. 실행 결과를 stdout으로 출력, exit code 전달

**stdout (치환 후 실행 결과):**
```
Success! Data written to: secret/foo
```
secret 값은 출력에 포함되지 않음.

---

## Vault 인증

우선순위 순서로 token을 탐색:
1. `VAULT_TOKEN` 환경변수
2. `~/.vault-token` 파일

`VAULT_ADDR` 환경변수로 주소 설정 (default: `http://localhost:8200`).

KV v2 mount는 `secret/` 고정 (MVP). 추후 설정 파일로 확장 가능.

---

## File Structure

```
lympht/
├── cmd/lympht/main.go          # CLI entry point (cobra)
├── internal/
│   ├── vault/client.go         # Vault KV v2 fetch
│   ├── inject/parser.go        # placeholder 파싱 및 치환
│   └── hook/intercept.go       # PreToolUse hook handler
├── docs/superpowers/specs/
├── go.mod
├── Makefile                    # build, install targets
└── README.md
```

---

## Security Properties

- secret 값은 `lympht` 프로세스 메모리에만 존재
- 치환된 명령 문자열은 LLM 컨텍스트로 반환되지 않음
- `lympht check`는 값 길이만 노출 (값 자체는 마스킹)
- VAULT_TOKEN은 lympht가 직접 읽음 — Claude가 token을 볼 필요 없음

---

## Out of Scope (MVP)

- 자체 secret 저장소 (Phase 2)
- 동적 비밀번호 생성 / TOTP (Phase 2)
- 모바일 Authority 인증 (Phase 2)
- output 스트림 필터링 (명령 출력에 secret이 포함되는 경우)
- 설정 파일 (KV mount, 다중 Vault 등)
