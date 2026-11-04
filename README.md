

## Runtime substitution mode (`LYMPHT_HOOK_MODE=runtime`)

By default the PreToolUse hook substitutes `{{lympht:...}}` values into the command it returns, and the harness stores that hook output in the session transcript (`hook_success`), so the plaintext persists there. With `LYMPHT_HOOK_MODE=runtime` the hook still validates every placeholder (a 403 or missing field blocks as before) but returns only `lympht run --b64 <original command>`. `lympht run` resolves the values at execution time, hands the script to bash through an anonymous pipe (`/dev/fd/3`, so nothing in argv) and masks the values (and their base64 / URL-encoded forms) in stdout and stderr with `{{lympht:redacted}}`. Limit: a command that transforms the value before printing it (reverse, split) is not caught. Default stays the old behaviour until the mode is switched on deliberately. Spec: geneso `neunexus/spec/2026-11-04-lympht-hook-no-plaintext-in-transcript-design.md`.

Behaviour differences in runtime mode:

- `cd` and `export` inside a wrapped command do not carry over to the next command (it runs in a child shell via `lympht run`); commands without placeholders are unaffected.
- Values shorter than 4 bytes are not masked in the output (they would match ordinary text).
