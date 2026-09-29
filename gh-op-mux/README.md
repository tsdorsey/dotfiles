# gh-op-mux

One `gh` on PATH for humans and agents. The binary is a client of a
long-lived multiplexer that runs `op plugin run -- <real-gh> …` on a
single unused PTY so 1Password sees one terminal session.

See [spec.md](spec.md) for the full contract.

## Prerequisites

1. 1Password desktop app with **Settings → Developer → Integrate with 1Password CLI** enabled.
2. `op plugin init gh` has already chosen a **global default** credential. If that is missing, `op` may draw a picker on the hidden PTY and hang the multiplexer. That is a setup bug, not something this tool can recover from.
3. No leftover `gh auth login` token in the keychain or `~/.config/gh/hosts.yml`. Raw Homebrew `gh` must not silently succeed.
4. This `bin/gh` is on PATH ahead of Homebrew (`gh.zsh` prepends `$ZSHDOT/bin`).

## Overrides

No config file. Optional env:

- `GH_OP_MUX_GH` — real `gh` binary (default: `/opt/homebrew/bin/gh`, then `/usr/local/bin/gh`)
- `GH_OP_MUX_OP` — `op` binary (default: `PATH`)
- `GH_OP_MUX_SOCK` — unix socket path (default: `~/Library/Caches/gh-op-mux/mux.sock`)

## Human check

First `gh` after unlocking 1Password: one biometric dialog. A second `gh`: none.
Locking the 1Password app revokes CLI access. The multiplexer stays up.
