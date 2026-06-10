# lympht

LLM-safe Vault secret injector for Claude Code.

Intercepts Bash tool calls and substitutes `{{lympht:path#field}}` placeholders
with Vault KV v2 values before execution. The LLM context never contains actual secret values.

## How it works

1. Claude writes a command with a placeholder:
   ```bash
   vault kv put secret/foo pass="{{lympht:neunexus/foo#password}}"
   ```
2. The Claude Code PreToolUse hook calls `lympht hook-intercept`
3. lympht fetches the value from Vault and returns a modified command
4. Claude Code executes the substituted command — LLM sees only the placeholder

## Installation

```bash
git clone https://github.com/tojiuni/lympht
cd lympht
make install
```

## Vault configuration

```bash
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=<your-token>
# or: vault login
```

## Claude Code hook registration

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

## Commands

| Command | Description |
|---------|-------------|
| `lympht hook-intercept` | PreToolUse hook entry point (stdin → stdout) |
| `lympht inject -- <cmd>` | Resolve placeholders and print substituted command |
| `lympht check <path>` | List field names at a Vault path (values masked) |

## Placeholder format

```
{{lympht:<vault-kv-path>#<field>}}

Example:
{{lympht:neunexus/cloudbro/postgres-gopedia#password}}
```
