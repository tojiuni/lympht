# lympht

LLM-safe Vault secret injector for Claude Code.

Intercepts Bash tool calls and substitutes `{{lympht:path#field}}` placeholders with Vault KV v2 values immediately before execution — the LLM context never contains actual secret values.

## How it works

```
Claude writes:   PGPASSWORD={{lympht:neunexus/db#password}} psql -h localhost ...
                                        ↓  PreToolUse hook
lympht injects:  PGPASSWORD='s3cr3t!pw' psql -h localhost ...   ← not in LLM context
Claude Code executes the resolved command
```

1. Claude writes a Bash command using `{{lympht:path#field}}` placeholders — the placeholder is just text, no secret ever appears in the conversation.
2. Before Claude Code runs the command, the `PreToolUse` hook calls `lympht hook-intercept`.
3. lympht fetches each secret from Vault, shell-quotes the value, and returns the modified command.
4. Claude Code executes the substituted command. The LLM only ever sees the placeholder.

### Shell-safe injection

Substituted values are automatically wrapped in single quotes with proper escaping, so passwords containing `$`, spaces, `"`, `` ` ``, `\`, `!`, or `'` are passed through intact.

| Written by LLM | After injection (example value `p@ss'w0rd`) |
|---|---|
| `PGPASSWORD={{lympht:p#k}}` | `PGPASSWORD='p@ss'\''w0rd'` |
| `PGPASSWORD='{{lympht:p#k}}'` | `PGPASSWORD='p@ss'\''w0rd'` |
| `PGPASSWORD="{{lympht:p#k}}"` | `PGPASSWORD='p@ss'\''w0rd'` |

Placeholders embedded inside a larger quoted string (e.g. `"Bearer {{lympht:...}}"`) use raw substitution to preserve the surrounding string context.

---

## Installation

```bash
git clone https://github.com/tojiuni/lympht
cd lympht
make install   # builds and copies binary to $GOPATH/bin or /usr/local/bin
```

## Setup

### 1. Vault connection

lympht reads `VAULT_ADDR` and `VAULT_TOKEN` (or `~/.vault-token`).

```bash
export VAULT_ADDR=http://localhost:8200

# Verify connection
curl -s $VAULT_ADDR/v1/sys/health | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed'))"
# false = healthy
```

If Vault is running in a Kubernetes cluster:
```bash
kubectl port-forward -n vault svc/vault 8200:8200 &
```

### 2. Claude Code hook registration

Add to `~/.claude/settings.json`:

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

Verify:
```bash
cat ~/.claude/settings.json | python3 -m json.tool | grep -A5 PreToolUse
```

---

## Placeholder format

```
{{lympht:<vault-kv-v2-path>#<field>}}
```

- **path** — KV v2 path relative to the `secret/` mount. Example: `neunexus/cloudbro/postgres-gopedia`
- **field** — key name within the secret. Use `lympht check <path>` to list available fields without revealing values.

---

## Usage examples

### Database password (psql, mysql, etc.)

```bash
# Unquoted — simplest form
PGPASSWORD={{lympht:neunexus/cloudbro/postgres-gopedia#password}} psql \
  -h localhost -p 5433 -U gopedia -d gopedia_dev \
  -c "SELECT COUNT(*) FROM c4_entity WHERE project_id = 57"

# Single-quoted — explicit form, identical result
PGPASSWORD='{{lympht:neunexus/cloudbro/postgres-gopedia#password}}' psql -h localhost -U gopedia
```

### HTTP API calls

```bash
# Bearer token in header
curl -H "Authorization: Bearer {{lympht:neunexus/someservice#token}}" \
  https://api.example.com/data

# Basic auth
curl -u {{lympht:neunexus/registry#username}}:{{lympht:neunexus/registry#password}} \
  https://registry.example.com/v2/
```

### Container registry login

```bash
docker login artifacts.toji.homes \
  -u {{lympht:neunexus/registry#username}} \
  --password-stdin <<< {{lympht:neunexus/registry#password}}
```

### Multiple placeholders in one command

```bash
kubectl create secret generic my-secret \
  --from-literal=db-pass={{lympht:neunexus/cloudbro/postgres-gopedia#password}} \
  --from-literal=api-key={{lympht:neunexus/someservice#token}}
```

### Git / SSH operations

```bash
GIT_SSH_COMMAND="ssh -i <(echo '{{lympht:neunexus/git-ssh#private_key}}')" \
  git clone git@github.com:org/repo.git
```

---

## Commands

### `lympht check <path>`

Lists field names at a Vault path. **Safe for LLM use** — only field names are returned, values are never shown.

```bash
lympht check neunexus/cloudbro/postgres-gopedia
# Fields at neunexus/cloudbro/postgres-gopedia:
#   ✓ host
#   ✓ password
#   ✓ username
```

Use this to discover what fields a secret has before writing a placeholder.

### `lympht hook-intercept`

PreToolUse hook entry point. Reads a Claude Code tool call JSON from stdin, substitutes placeholders, and writes the modified response to stdout. Passes through silently if no placeholders are present or if the tool is not Bash.

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"echo {{lympht:neunexus/github-webhook#secret}}"}}' \
  | lympht hook-intercept
```

This command is called automatically by the Claude Code hook — you do not need to run it manually.

### `lympht inject -- <command>`

Resolves placeholders and **prints** the substituted command. Since the actual value is printed to stdout, run this outside the LLM context using the `!` prefix in Claude Code.

```bash
# Must use ! prefix in Claude Code to keep values out of LLM context
! lympht inject -- curl -u admin:{{lympht:neunexus/foo#password}} https://example.com
```

---

## Security model

| Action | Safe for LLM? | Notes |
|--------|:---:|---|
| Write `{{lympht:path#field}}` in a command | ✅ | Placeholder only, no value |
| `lympht check <path>` | ✅ | Returns field names only |
| `lympht hook-intercept` (automatic) | ✅ | Called by hook; output goes to Claude Code, not LLM |
| `lympht inject -- <cmd>` | ❌ | Prints resolved value — use `!` prefix |
| `vault kv get <path>` | ❌ | Prints values — use `!` prefix |
| `kubectl get secret -o yaml` | ❌ | Prints values — use `!` prefix |

**Rule of thumb:** any command that would print a secret to stdout must be run with `!` in Claude Code so its output stays out of the LLM conversation.

---

## Troubleshooting

**`connection refused` — Vault unreachable**
```bash
kubectl port-forward -n vault svc/vault 8200:8200 &
sleep 2 && lympht check neunexus/github-webhook
```

**`403 permission denied` — token expired or lacks access**
```bash
# Renew outside LLM context
! vault login -method=token
```

**`vault returned 404` — path not found**

Check for typos and explore from the parent path:
```bash
lympht check neunexus            # top-level keys
! vault kv list secret/neunexus/ # full listing (values hidden, paths shown)
```

**Placeholder not substituted (command runs with literal `{{lympht:...}}`)**

Verify the hook is registered:
```bash
cat ~/.claude/settings.json | python3 -m json.tool | grep hook-intercept
```
