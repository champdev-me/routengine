# Command line

On **Windows** the installer puts `RoutEngine.exe` in `C:\Program Files\RoutEngine`. On **macOS and Linux** the program is `routengine` (see [macOS and Linux](#macos-and-linux) below). Most people only use the window. The commands are for scripts, for servers and for checking status.

Run the program with a command (`RoutEngine.exe` on Windows, `routengine` elsewhere):

| Command | What it does |
| ------- | ------------ |
| `app` | The RoutEngine app: tray icon, window and routing in one program. This is the default. |
| `tray` | The same, on Windows. |
| `run` | Turn routing on for the whole PC in a console window. Ctrl+C, or `off` from another window, turns it off. Asks for administrator rights. |
| `on`, `off` | Start or stop routing in the running app. |
| `quit` | Stop routing and quit the running app. |
| `status` | Show whether routing is on, and the load on each adapter. |
| `list` | Show the network adapters that were found. |
| `socks` | A plain SOCKS5 proxy on `127.0.0.1:1080`. Needs no administrator rights. Per app. |
| `autostart on`, `off`, `status` | Start the tray at sign-in. No admin prompt. |
| `stats on`, `off`, `status` | Send anonymous usage statistics, or not. Nothing is sent until you agree. See [Privacy](privacy.md). |
| `cleanup` | Remove leftover routes after a crash. Needs administrator rights. |
| `license add <key>`, `status`, `remove` | Window apps only: enter, check or remove the Pro license key. `add` also asks the licensing server at once. |
| `limit`, `limit <adapter> <down> [<up>]`, `limit <adapter> off` | Window apps only (Pro): list the speed limits of adapters, set one in Mbit/s (one number caps both ways; `off` for one direction leaves it free), or remove it. See [Limits and caps](limits-and-caps.md#speed-limit-per-adapter-pro). |
| `machine-code` | Window apps only: print this PC's code, to send when you buy Pro. |
| `install`, `uninstall` | Linux: install or remove a systemd service that starts at boot. macOS: let the menu-bar app start without a password. |
| `version` | Print the version. |

In the window apps `on` refuses to start routing when Start is blocked, for the reasons in [License](license.md#if-the-license-data-is-changed). The command line has no license, so nothing blocks it.

## macOS and Linux

`routengine` is the same engine. The command-line build has no window and no desktop libraries, so it runs on a bare server. It is free for any use, work included, on any number of apps, and has no license key and no Pro edition. The app packages contain the same commands, plus the license ones.

```
curl -fsSL https://routengine.net/install.sh | sh     # or the .deb, .rpm and .tar.gz files on the release page
brew install champdev-me/tap/routengine               # Homebrew, macOS and Linux
sudo apt install routengine                           # or dnf, zypper: add the repository first, see the README
sudo routengine install                               # Linux: runs at boot as the systemd service "routengine"
```

**Files.** As root on Linux (`sudo`, or the service) they are in `/etc/routengine`. Otherwise they are in your config folder: `~/.config/routengine` on Linux, `~/Library/Application Support/routengine` on macOS. On macOS the app runs as root but uses your folder, so the app and the terminal agree. `ROUTENGINE_DIR` overrides all of this.

| File | What it is |
| ---- | ---------- |
| `config.json` | The rules. Same format as `rules.json` on Windows, and it wins when both exist. |
| `license.key` | The window app's Pro license key, if it has one. The command line does not read it. |
| `usage.json` | The data-used history. |

**Rules.** An app is a process name (`curl`) or a full path. On macOS a name ending in `.app`, such as `Google Chrome.app`, matches every program inside that app, so a browser's helper processes follow it.

```json
{
  "default_adapter": "eth0",
  "rules": [
    { "exe": "qbittorrent", "adapter": "eth1", "failover": ["default"] },
    { "exe": "curl", "balance": ["eth0", "wlan0"] }
  ]
}
```

The rules file is reloaded within a second of any change. Everything else balances across all adapters, as in the window app.

**The service.** `sudo routengine install` copies the program to `/usr/local/bin`, writes `/etc/systemd/system/routengine.service` and starts it. Logs: `journalctl -u routengine`. `sudo routengine uninstall` removes both and keeps `/etc/routengine`. The packages (`.deb`, `.rpm`) install the same service but do not enable it: `sudo systemctl enable --now routengine`.

**Build from source.** The command-line source is in this repository under the [PolyForm Noncommercial License](../LICENSE). You need Go (the version in `go.mod`); on macOS also the Xcode command line tools, because looking up which program owns a connection uses the system library.

```
go build -tags nogui -o routengine ./cmd/routengine
go test -tags nogui ./...
```

The `nogui` tag leaves out the window and tray, which are not part of this repository, and the license code and Pro features of the window apps, which are not either.

**Updates.** `routengine run` writes a line to its log when a newer version exists. It never installs anything by itself: use your package manager, Homebrew, or the download.

**Limits.** On Linux a program of another user is only named when RoutEngine runs as root, which the service does. Anything that cannot be named is balanced and counted under "(unidentified)". There is no app limit: every app can have its own rule, a speed limit and a data cap. DNS per app, the fastest adapter, speed limits per adapter and usage history beyond today are Pro features of the window apps.
