# lympht Runbook

LLM이 Vault secret 값을 직접 보지 않고도 secret을 활용할 수 있는 Claude Code Bash 훅.

## 동작 원리

```
Claude가 작성:  vault kv put secret/foo pass="{{lympht:vault:neunexus/foo#password}}"
                                                ↓ PreToolUse hook
lympht가 교체:  vault kv put secret/foo pass="실제값"  ← LLM 컨텍스트에 없음
Claude Code가 실행
```

LLM 컨텍스트에는 placeholder만 존재. 실제 값은 hook이 실행 직전에 주입.

---

## 사전 조건

### Vault port-forward 확인

```bash
curl -s http://localhost:8200/v1/sys/health | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed','?'))"
# false 이면 정상
```

연결 안 되면:
```bash
kubectl port-forward -n vault svc/vault 8200:8200 &
```

상세: `neunexus/operations/infra/mcp-portforward.md`

### Vault 토큰 확인

```bash
cat ~/.vault-token | wc -c   # 토큰 존재 여부만 확인
```

---

## 명령어

### `lympht check <vault-path>`

Vault 경로의 **필드명만** 확인 (값은 보이지 않음 — LLM이 사용해도 안전).

```bash
lympht check vault:neunexus/github-webhook
# Fields at vault:neunexus/github-webhook:
#   ✓ secret
```

### `lympht hook-intercept`

Claude Code PreToolUse hook 진입점. stdin으로 JSON을 받아 placeholder를 교체 후 stdout 출력.

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:vault:neunexus/github-webhook#secret}}"}}' \
  | lympht hook-intercept
# {"decision":"modify","tool_input":{"command":"echo <실제값>"}}
```

placeholder가 없으면 빈 출력(passthrough), Bash 외 툴도 빈 출력(passthrough).

### `lympht inject "<command>"`

placeholder를 교체해 **터미널에 출력** — 값이 화면에 표시되므로 LLM 컨텍스트 밖(`! <cmd>`)에서만 사용.

```bash
# 반드시 ! 접두사로 실행 (LLM context 차단)
! lympht inject "curl -u admin:{{lympht:vault:neunexus/foo#password}} https://example.com"
```

---

## Placeholder 형식

```
{{lympht:vault:<vault-path>#<field>}}    ← Vault KV v2
{{lympht:k8s:<ns>/<secret-name>#<key>}} ← kubectl secret (auto base64-decoded)
```

- `vault-path`: KV v2 mount(`secret`) 기준 경로. 예: `neunexus/github-webhook`
- `field` / `key`: secret의 키 이름. `lympht check vault:<path>` 또는 `lympht check k8s:<ns>/<name>`으로 확인 가능.

### 예시

```bash
# DB 비밀번호
psql "postgresql://app:{{lympht:vault:neunexus/cloudbro/postgres-gopedia#password}}@localhost/db"

# API 토큰
curl -H "Authorization: Bearer {{lympht:vault:neunexus/someservice#token}}" https://api.example.com

# 여러 placeholder
docker login registry.example.com \
  -u {{lympht:vault:neunexus/registry#username}} \
  -p {{lympht:vault:neunexus/registry#password}}
```

### kubectl Secret 예시

```bash
# K8s secret 키 목록 확인 (값 없음)
lympht check k8s:cogito-svc/cogito-s3
# Fields at k8s:cogito-svc/cogito-s3:
#   ✓ access-key
#   ✓ bucket-name
#   ✓ secret-key

# 여러 backend 혼용
docker login registry.example.com \
  -u {{lympht:vault:neunexus/registry#username}} \
  -p {{lympht:k8s:myns/registry-creds#password}}
```

---

## Claude Code 훅 등록 확인

`~/.claude/settings.json`에 아래가 있어야 함:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "lympht hook-intercept" }]
      }
    ]
  }
}
```

확인:
```bash
cat ~/.claude/settings.json | python3 -c "import json,sys; h=json.load(sys.stdin); print(h.get('hooks',{}).get('PreToolUse','NOT SET'))"
```

---

## 알려진 Vault 경로 (neunexus)

경로 구조만 기록. 실제 값은 절대 기록하지 않음.

| 경로 | 용도 | 필드 확인 |
|------|------|-----------|
| `neunexus/github-webhook` | Tekton EventListener HMAC secret | `lympht check vault:neunexus/github-webhook` |
| `neunexus/registry` | artifacts.toji.homes 레지스트리 인증 | `lympht check vault:neunexus/registry` |
| `neunexus/ghcr` | GitHub Container Registry 인증 | `lympht check vault:neunexus/ghcr` |
| `neunexus/git-ssh` | Git SSH 키 | `lympht check vault:neunexus/git-ssh` |
| `neunexus/cloudbro/postgres-gopedia` | gopedia-bro PostgreSQL | `lympht check vault:neunexus/cloudbro/postgres-gopedia` |

새 경로 탐색: `lympht check <parent-path>` 또는 `! vault kv list secret/neunexus/`

---

## 트러블슈팅

### `connection refused` (Vault 연결 실패)

```bash
kubectl port-forward -n vault svc/vault 8200:8200 &
sleep 2 && lympht check vault:neunexus/github-webhook
```

### `403 permission denied`

토큰 만료 또는 권한 부족.

```bash
# 토큰 갱신 (LLM context 밖에서)
! vault login -method=token   # 또는 적절한 auth method
```

### `vault returned 404`

경로가 없거나 오타. `lympht check`로 상위 경로부터 탐색.

### HMAC signature check failed (Tekton webhook)

Vault secret과 k8s `github-webhook-secret`이 불일치. k8s 값으로 webhook 재등록:

```bash
# LLM context 밖에서 실행
! WEBHOOK_SECRET=$(kubectl get secret github-webhook-secret -n tekton-pipelines \
    -o jsonpath='{.data.secret}' | base64 -d) && \
  gh api repos/<org>/<repo>/hooks/<hook-id> \
    --method PATCH \
    -f "config[secret]=$WEBHOOK_SECRET"
```

---

## 보안 규칙

1. **LLM에게 실제 값을 보여주지 않는다** — `lympht inject` 결과, `vault kv get` 출력 등은 반드시 `!` 접두사로 실행
2. **`lympht check`는 안전** — 필드명만 반환
3. **placeholder는 안전** — `{{lympht:path#field}}` 형태는 LLM이 쓸 수 있음
4. **runbook에 값 기록 금지** — 이 문서 포함 모든 문서에 실제 secret 값 절대 기록하지 않음
