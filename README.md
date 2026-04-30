<div align="center">
    <h1>LocalSend TUI</h1>
    <h4>✨ A headless / TUI LocalSend client written in Go ✨</h4>
</div>

> Fork of [meowrain/localsend-go](https://github.com/meowrain/localsend-go) with HTTPS support and a stable certificate fingerprint, so the official LocalSend mobile/desktop apps can connect with their default (encrypted) settings.

## 文档 | Document | ドキュメント

[中文](doc/README_zh.md) | [English](doc/README_en.md) | [日本語](doc/README_jp.md)

## 版本说明 | Version Notes | バージョン情報

- v1.2.2 - 当前最新版本 | Current Version | 現行バージョン
- [v1.1.0](doc/version1.1.0/) - 历史版本 | Historical Version | 過去のバージョン

## 快速开始 | Quick Start | クイックスタート

### 使用go install安装

```bash
go install github.com/tingkai-c/localsend-cli@latest
```

### 从包管理器安装 | Install from Package Manager | パッケージマネージャーからインストール

#### Arch Linux
```bash
yay -Syy
yay -S localsend-go
```

### 从源码编译 | Build from Source | ソースからビルド

```bash
git clone https://github.com/meowrain/localsend_cli.git
cd localsend_cli
make build
```

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

Examples:

```bash
# One-off: receive into a specific dir without editing the config
LOCALSEND_CLI_OUTPUT_DIR=/tmp/inbox localsend-cli receive

# Persistent: edit the config file and uncomment the keys you want to set
$EDITOR ~/.config/localsend-cli/config.yaml

# Quick override
localsend-cli --output-dir=/tmp/inbox --port=12345 receive
```


## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date)](https://www.star-history.com/#meowrain/localsend-go&Date)

