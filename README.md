<div align="center">
    <h1>LocalSend CLI</h1>
    <h4>✨ A headless / CLI LocalSend client written in Go ✨</h4>
</div>

> Fork of [meowrain/localsend-go](https://github.com/meowrain/localsend-go) with HTTPS support and a stable certificate fingerprint, so the official LocalSend mobile/desktop apps can connect with their default (encrypted) settings.

## 文档 | Document | ドキュメント

[中文](doc/README_zh.md) | [English](doc/README_en.md) | [日本語](doc/README_jp.md)

## 版本说明 | Version Notes | バージョン情報

- v1.3.2 - 当前最新版本 | Current Version | 現行バージョン
- [v1.1.0](doc/version1.1.0/) - 历史版本 | Historical Version | 過去のバージョン

## Install

No Go toolchain required for the one-liners below — they download the right pre-built binary from [GitHub Releases](https://github.com/tingkai-c/localsend-cli/releases/latest), verify its SHA-256 against `checksums.txt`, and drop it on your `PATH`.

### Linux & macOS

```bash
curl -fsSL https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.sh | sh
```

Defaults to `/usr/local/bin`, falling back to `~/.local/bin` if the former isn't writable. Override with env vars:

```bash
# Pin a specific version
VERSION=v1.3.2 curl -fsSL https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.sh | sh

# Install to a custom directory
INSTALL_DIR=$HOME/bin curl -fsSL https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.sh | sh
```

If you'd rather review the script before piping it to `sh`:

```bash
curl -fsSLO https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.sh
less install.sh
sh install.sh
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/tingkai-c/localsend-cli/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\Programs\localsend-cli` and adds it to your **user** `PATH` (no admin needed). Override with `$env:VERSION` or `$env:INSTALL_DIR` before invoking.

### Manual download

Grab the archive for your OS/arch from the [latest release](https://github.com/tingkai-c/localsend-cli/releases/latest), verify it against `checksums.txt`, extract, and put `localsend-cli` on your `PATH`. Asset names follow the pattern `localsend-cli_<version>_<os>_<arch>.{tar.gz,zip}`.

### Arch Linux (AUR)

```bash
yay -S localsend-cli
```

### Build from source (requires Go 1.22+)

```bash
go install github.com/tingkai-c/localsend-cli@latest
# or
git clone https://github.com/tingkai-c/localsend-cli.git
cd localsend-cli && make build
```


## Interactive TUI dashboard

Run `localsend-cli` with no subcommand to open the Bubble Tea dashboard. The dashboard keeps the LocalSend HTTPS server and discovery broadcast running while giving you terminal-native workflows for:

- sending files/folders/text-compatible payloads;
- selecting one or multiple nearby recipients (`Space` toggles recipients, `Enter` sends to selected/current);
- accepting, rejecting, or trusting unknown incoming transfers from an in-TUI approval modal;
- reviewing/deleting transfer history;
- reviewing/forgetting trusted senders;
- viewing settings, paths, Quick Save state, and keybindings.

The TUI intentionally is not a desktop pixel clone: it is a keyboard-first companion that preserves LocalSend v2 protocol compatibility and keeps headless commands excellent.

## Configuration

Settings are resolved with the precedence **command-line flag > environment variable > config file > built-in default**.

The config file is auto-generated on first run at:

- Linux / WSL: `~/.config/localsend-cli/config.yaml`
- macOS: `~/Library/Application Support/localsend-cli/config.yaml`
- Windows: `%AppData%\localsend-cli\config.yaml`

| Setting | Config key | Env var | Flag | Default |
|---|---|---|---|---|
| Device alias | `device_name` | `LOCALSEND_CLI_DEVICE_NAME` | `--device-name` | random `Adjective Noun` |
| HTTPS port | `port` | `LOCALSEND_CLI_PORT` | `--port` | `53317` |
| Output directory | `output_dir` | `LOCALSEND_CLI_OUTPUT_DIR` | `--output-dir` | `~/Downloads/localsend-cli` |
| Quick Save | `quick_save` | `LOCALSEND_CLI_QUICK_SAVE` | `--quick-save` | `false` |

Examples:

```bash
# One-off: receive into a specific dir without editing the config
LOCALSEND_CLI_OUTPUT_DIR=/tmp/inbox localsend-cli receive

# Send a short text message as a protocol-compatible text payload
localsend-cli send-text "hello from the terminal"

# Launch the TUI dashboard
localsend-cli

# Persistent: edit the config file and uncomment the keys you want to set
$EDITOR ~/.config/localsend-cli/config.yaml

# Quick override
localsend-cli --output-dir=/tmp/inbox --port=12345 receive
```

## Approval prompt

`receive` mode is **secure-by-default**: every incoming session blocks on an interactive prompt before any file is written.

```
[localsend] Incoming transfer
  From:        Alice's Phone (fingerprint a1b2c3d4e5f6…)
  Files:       2, total 1.4 MiB
  - report.pdf (1.2 MiB)
  - notes.txt (217 KiB)
Accept files? [y]es / [n]o / [a]lways:
```

- `y` accepts this session.
- `n` rejects (sender sees `403`).
- `a` accepts and persists the sender's TLS fingerprint to `<config-dir>/localsend-cli/trusted.yaml`. Future sessions from the same fingerprint skip the prompt.
- No answer within **60 seconds** → reject (sender sees `403`).
- A second incoming session while the prompt is up gets `409 Blocked by another session`.

For headless / daemon use, the prompt cannot run (no TTY) so unrecognised senders are rejected immediately. Enable `quick_save: true` (or set `LOCALSEND_CLI_QUICK_SAVE=1`, or pass `--quick-save`) to auto-accept everything — equivalent to the pre-1.3 behavior. When the TUI dashboard is active, unknown sender approval is routed through the dashboard modal instead of stdin, avoiding prompt collisions.

Manage the trust list:

```bash
# List currently-trusted senders
localsend-cli trusted

# Forget by alias (case-insensitive), full fingerprint, or fingerprint prefix (>= 8 chars)
localsend-cli forget "Alice's Phone"
localsend-cli forget a1b2c3d4
```

## Transfer history

Completed sends and receives are recorded in a local transfer history file at:

- Linux / WSL: `~/.config/localsend-cli/history.json`
- macOS: `~/Library/Application Support/localsend-cli/history.json`
- Windows: `%AppData%\localsend-cli\history.json`

Use the CLI to inspect or clear it:

```bash
localsend-cli history
localsend-cli history-clear
```

The interactive dashboard also exposes transfer history and trusted senders:

- History screen: `j/k` move, `d` deletes the selected record, `c` clears all records.
- Trusted screen: `j/k` move, `d` or `x` forgets the selected trusted sender.

## Headless/TUI behavior notes

- No subcommand launches the TUI dashboard.
- `receive` keeps the classic stdin approval prompt for explicit CLI mode.
- TUI mode installs a channel-backed approval provider so HTTP receive handlers never read stdin while Bubble Tea owns the terminal.
- Headless unknown senders without Quick Save or prior trust are rejected with `403`; concurrent approval requests return `409`.
- Transfer progress is emitted as UI-neutral events and rendered by the CLI adapter today; TUI transfer screens can consume the same event stream.


## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date)](https://www.star-history.com/#meowrain/localsend-go&Date)

