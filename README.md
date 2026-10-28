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

pmx is a terminal email client built for people who live in the keyboard. It connects to any IMAP/SMTP server — including Gmail and Office 365 via OAuth2 — syncs your mail locally, and gets out of the way. No Electron. No mouse required. Just fast, distraction-free email in your terminal.

---

## Features

**Zero-friction navigation**  
A Vim-inspired modal interface with a [whichkey](https://github.com/folke/which-key.nvim)-style popup for every command. Hit `space` at any time to discover what's available — you never need to remember a cheat sheet.

**Multi-account, always in sync**  
Manage multiple email accounts side by side. pmx syncs in the background using IMAP IDLE for push delivery where supported, falling back to polling automatically. An optional daemon (`pmxd`) keeps mail fresh even when the TUI isn't open.

**Write with your editor**  
Compose flows through three stages: fill in the envelope (from, to, subject), write the body in `$EDITOR`, then preview with live markdown rendering before you send. Attach files. Toggle markdown on or off per message.

**OAuth2 support built in**  
Authenticate with Gmail or Office 365 through a browser-based OAuth2 PKCE flow. Tokens are stored in your system keyring — your credentials never touch config files.

**Folder and account drawers**  
Slide-out panels give you instant access to folders and accounts without leaving your current view. Auto-detects inbox, sent, drafts, archive, and trash via RFC 6154 SPECIAL-USE attributes.

**Polished status line**  
A persistent header shows your active account, current folder, unread counts, and live sync progress. A context-aware status bar below always tells you what keys do what.

---

## Install

### Pre-built binaries

Download the latest release for your platform from the [Releases](https://github.com/5Sigma/pmx/releases) page. Binaries are provided for Linux and macOS on both amd64 and arm64, plus Windows (amd64).

**Linux (amd64)**
```sh
curl -Lo pmx https://github.com/5Sigma/pmx/releases/latest/download/pmx-linux-amd64
curl -Lo pmxd https://github.com/5Sigma/pmx/releases/latest/download/pmxd-linux-amd64
chmod +x pmx pmxd
sudo mv pmx pmxd /usr/local/bin/
```

**macOS (Apple Silicon)**
```sh
curl -Lo pmx https://github.com/5Sigma/pmx/releases/latest/download/pmx-darwin-arm64
curl -Lo pmxd https://github.com/5Sigma/pmx/releases/latest/download/pmxd-darwin-arm64
chmod +x pmx pmxd
sudo mv pmx pmxd /usr/local/bin/
```

**macOS (Intel)**
```sh
curl -Lo pmx https://github.com/5Sigma/pmx/releases/latest/download/pmx-darwin-amd64
curl -Lo pmxd https://github.com/5Sigma/pmx/releases/latest/download/pmxd-darwin-amd64
chmod +x pmx pmxd
sudo mv pmx pmxd /usr/local/bin/
```

`pmxd` is the optional background daemon — skip it if you don't need background sync.

### NixOS (flake)

pmx ships a Nix flake that provides both binaries and a Home Manager
module for the `pmxd` user service. It is not in nixpkgs — install
directly from this repo.

**Quick try (no install):**

```sh
nix run github:5Sigma/pmx            # launch pmx TUI
nix run github:5Sigma/pmx#pmxd       # run the daemon in the foreground
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

Both `pmx` and `pmxd` are on your PATH after switching.

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
go build ./cmd/pmxd     # optional background daemon
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

`pmxd` syncs your mail on a schedule and sends desktop notifications for new messages. You can run it manually or wire it into your system's service manager so it starts at login.

**Run manually**
```sh
pmxd
```

**Linux — systemd user service**

A service unit is included in `contrib/pmxd.service`. Copy it into your user unit directory and enable it:

```sh
mkdir -p ~/.config/systemd/user
cp /path/to/contrib/pmxd.service ~/.config/systemd/user/pmxd.service
```

If `pmxd` is not at `~/.local/bin/pmxd`, edit the `ExecStart` line to match your install path, then:

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
    <string>/usr/local/bin/pmxd</string>
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

Adjust `ProgramArguments` if `pmxd` is installed elsewhere, then load it:

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
