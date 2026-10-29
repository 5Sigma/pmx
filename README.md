<div align="center">

```
██████╗ ███╗   ███╗██╗  ██╗
██╔══██╗████╗ ████║╚██╗██╔╝
██████╔╝██╔████╔██║ ╚███╔╝ 
██╔═══╝ ██║╚██╔╝██║ ██╔██╗ 
██║     ██║ ╚═╝ ██║██╔╝ ██╗
╚═╝     ╚═╝     ╚═╝╚═╝  ╚═╝
```

**A keyboard-driven terminal email client that doesn't get in your way.**

[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)](#license)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey?style=flat-square)]()
[![Arch](https://img.shields.io/badge/arch-x86__64%20%7C%20arm64-lightgrey?style=flat-square)]()

</div>

---

pmx is a terminal email client built to be easy to use and polished.  Living in a terminal doesn't mean sacrifcing nice tools.

---

## Features

**Zero-friction navigation**  
A Vim-inspired modal interface with a [whichkey](https://github.com/folke/which-key.nvim)-style popup for every command.

**Multi-account, always in sync**  
Manage multiple email accounts side by side. 

**Write with your editor**  
Compose emails using your perferred CLI editor: neovim, vim, emacs, etc.

**OAuth2 support built in**  
Authenticate with Gmail or Office 365 through a browser-based OAuth2 PKCE flow.  This is a *bring your own client ID* flow.


---

## Install

### Pre-built binaries

Download the latest release for your platform from the [Releases](https://github.com/5Sigma/pmx/releases) page. Binaries are provided for Linux and macOS on both amd64 and arm64, plus Windows (amd64).

**Linux (amd64)**
```sh
curl -Lo pmx https://github.com/5Sigma/pmx/releases/latest/download/pmx-linux-amd64
chmod +x pmx
sudo mv pmx /usr/local/bin/
```

**macOS (Apple Silicon)**
```sh
curl -Lo pmx https://github.com/5Sigma/pmx/releases/latest/download/pmx-darwin-arm64
chmod +x pmx
sudo mv pmx /usr/local/bin/
```

**macOS (Intel)**
```sh
curl -Lo pmx https://github.com/5Sigma/pmx/releases/latest/download/pmx-darwin-amd64
chmod +x pmx
sudo mv pmx /usr/local/bin/
```

### NixOS (flake)

pmx ships a Nix flake that provides both binaries and a Home Manager
module for the `pmxd` user service. It is not in nixpkgs — install
directly from this repo.

**Quick try (no install):**

```sh
nix run github:5Sigma/pmx            # launch pmx TUI
nix run github:5Sigma/pmx -- daemon  # run the daemon in the foreground
```

**Install into your user profile:**

```sh
nix profile install github:5Sigma/pmx
```

**NixOS + Home Manager (recommended — enables the pmxd user service):**

Add the flake as an input in your system or Home Manager flake:

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url     = "github:NixOS/nixpkgs/nixos-unstable";
    home-manager.url = "github:nix-community/home-manager";
    pmx.url         = "github:5Sigma/pmx";
    # Pin pmx to your nixpkgs to avoid pulling a second channel:
    pmx.inputs.nixpkgs.follows = "nixpkgs";
  };
  # ...
}
```

Import the Home Manager module in your HM config and enable the daemon:

```nix
{ inputs, ... }:
{
  imports = [ inputs.pmx.homeManagerModules.pmxd ];

  services.pmxd = {
    enable    = true;
    # extraArgs = [ ];   # e.g. [ "--once" ]
    # autoStart = true;  # default; Linux: WantedBy=default.target, macOS: RunAtLoad
  };
}
```

Then rebuild:

```sh
home-manager switch
systemctl --user status pmxd     # confirm the daemon is running
journalctl --user -u pmxd -f     # tail its logs
```

`pmx` is on your PATH after switching. The daemon runs as `pmx daemon`.

**Overlay (without Home Manager):**

```nix
# in your NixOS configuration
nixpkgs.overlays = [ inputs.pmx.overlays.default ];
environment.systemPackages = [ pkgs.pmx ];
# To enable the user service manually:
#   systemctl --user link ${pkgs.pmx}/share/systemd/user/pmxd.service
#   systemctl --user enable --now pmxd
```

**Runtime notes:**

- `xdg-open` is bundled on the wrapper PATH — opening attachments and
  the OAuth2 browser flow work out of the box.
- Desktop notifications require a running D-Bus session (standard on
  NixOS graphical sessions).
- SecretService keyring (gnome-keyring, kwallet, keepassxc) is
  optional. If you don't run one, set `password_command` in
  `~/.config/pmx/config.toml` to pipe in your password via `pass`,
  `op`, `secret-tool`, etc.
- Config: `~/.config/pmx/config.toml` · Mail/data: `~/.local/share/pmx/`

**Supported platforms:** `x86_64-linux`, `aarch64-linux`,
`x86_64-darwin`, `aarch64-darwin`. The `pmxd` systemd unit is
Linux-only.

### From source

```sh
go install github.com/5Sigma/pmx@latest
```

### Build locally

```sh
git clone https://github.com/5Sigma/pmx
cd pmx
go build .
```

---

## Quick Start

### 1. Add an account

**Gmail**
```sh
pmx account add --provider gmail
```
Walks you through a browser-based OAuth2 flow. No app password or 2FA gymnastics required.

**Office 365**
```sh
pmx account add --provider o365
```
Uses the Thunderbird public client ID by default — no Azure app registration needed.

**Any IMAP/SMTP server**

Edit `~/.config/pmx/config.toml` directly:

```toml
[[accounts]]
name        = "personal"
email       = "you@example.com"
imap_host   = "imap.example.com"
imap_port   = 993
imap_tls    = true
smtp_host   = "smtp.example.com"
smtp_port   = 465
smtp_tls    = true
auth        = "password"
```

### 2. Open pmx

```sh
pmx
```

### 3. Run the background daemon *(optional)*

The daemon syncs your mail on a schedule and sends desktop notifications for new messages. You can run it manually or wire it into your system's service manager so it starts at login.

**Run manually**
```sh
pmx daemon
```

**Linux — systemd user service**

A service unit is included in `contrib/pmxd.service`. Copy it into your user unit directory and enable it:

```sh
mkdir -p ~/.config/systemd/user
cp /path/to/contrib/pmxd.service ~/.config/systemd/user/pmxd.service
```

If `pmx` is not at `~/.local/bin/pmx`, edit the `ExecStart` line to match your install path, then:

```sh
systemctl --user daemon-reload
systemctl --user enable --now pmxd
systemctl --user status pmxd       # confirm it's running
journalctl --user -u pmxd -f       # tail logs
```

**macOS — launchd**

Create `~/Library/LaunchAgents/io.5sigma.pmxd.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.5sigma.pmxd</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/pmx</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>StandardErrorPath</key>
  <string>/tmp/pmxd.log</string>
</dict>
</plist>
```

Adjust `ProgramArguments` if `pmx` is installed elsewhere, then load it:

```sh
launchctl load ~/Library/LaunchAgents/io.5sigma.pmxd.plist
launchctl list | grep pmxd     # confirm it loaded
```

---

## Configuration

Config lives at `~/.config/pmx/config.toml`.

```sh
pmx config edit      # open in $EDITOR
pmx config validate  # check for errors
pmx config path      # print the config file path
```

### Full reference

```toml
[general]
default_account  = "personal"
log_level        = "info"       # debug | info | warn | error
poll_interval    = "2m"
send_interval    = "1m"
send_max_attempts = 5

[daemon]
check_interval        = "15m"
notifications_enabled = true
notify_folders        = ["inbox"]

[theme]
primary  = "#7D56F4"
accent   = "#73F59F"
muted    = "#555577"
fg       = "#DDDDEE"
dim_fg   = "#888899"
error    = "#FF5F87"
warning  = "#FFAF5F"
success  = "#73F59F"

[features]
alt_screen    = true
mouse         = true
filter_enabled = true
show_status_bar = true
confirm_quit  = false

[keys]
quit       = ["q", "ctrl+c"]
open       = ["enter"]
back       = ["esc", "backspace"]
scroll_up  = ["up", "k"]
scroll_down = ["down", "j"]
filter     = ["/"]
refresh    = ["r"]
```

---

## MCP Server

`pmx mcp` exposes your email as an [MCP](https://modelcontextprotocol.io) tool server over stdio, letting AI agents (Claude Code, Claude Desktop, Cursor, Cline, Continue, Zed) read and manage mail without raw IMAP access.

### Setup

**Claude Code**

```sh
claude mcp add --scope user pmx -- pmx mcp
```

Restart any open Claude Code sessions to pick up the new server.

**Claude Desktop / other clients** — add to your MCP config:

```json
{
  "mcpServers": {
    "pmx": {
      "command": "pmx",
      "args": ["mcp"]
    }
  }
}
```

`pmx mcp` reads from the same local SQLite store as the TUI. If `pmxd` is not running it starts its own sync goroutines, so mutations (mark read, move, delete, send) still propagate to the server.

### Tool reference

| Tool | Description |
|------|-------------|
| `list_accounts` | List configured accounts (name, email — no credentials). |
| `list_folders` | List folders for an account with unread and total counts. |
| `list_messages` | List messages in a folder, newest first. Supports `unread_only`, `limit`, and `offset`. |
| `get_message` | Fetch a single message by UID — headers, body (`text`/`html`/`both`), attachment metadata. |
| `get_attachment` | Download an attachment by ID. Returns base64 content (≤ 10 MB) or an on-disk path for larger files. |
| `search_messages` | Full-text search (SQLite FTS5) across subject, sender, recipients, and body. |
| `unread_count` | Count unread messages — per folder, per account, or a global per-account map. |
| `message_count` | Count messages stored locally in a folder. |
| `compose_message` | Send a new email, with optional Markdown rendering and base64 attachments. |
| `reply` | Reply to a message; the original is quoted automatically. |
| `forward` | Forward a message; original body and attachments are included automatically. |
| `mark_read` / `mark_unread` | Toggle the read flag on a message. |
| `flag` / `unflag` | Star or un-star a message. |
| `move_message` | Move a message to a different folder. |
| `delete_message` | Move a message to Trash. |

All tools accept `account` (account name) plus either `folder` (exact IMAP mailbox name) or `role` (`inbox`, `sent`, `drafts`, `archive`, `trash`, `junk`) to identify a folder.

---

## Storage

pmx follows the XDG Base Directory spec.

| Path | Contents |
|------|----------|
| `~/.config/pmx/config.toml` | Configuration |
| `~/.cache/pmx/messages.db` | Local message store (SQLite, WAL mode) |
| `~/.cache/pmx/attachments/` | Downloaded attachments |
| `~/.cache/pmx/pmx.log` | Application log |

Credentials are stored in your system keyring (macOS Keychain, SecretService on Linux), never in flat files.

---

## License

MIT © [Joe Bellus](https://github.com/joebellus)
