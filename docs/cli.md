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
| `license add <key>`, `status`, `remove` | Enter, check or remove the Pro license key. `add` also asks the licensing server at once. |
| `machine-code` | Print this machine's code, to send when you buy Pro. |
| `install`, `uninstall` | Linux: install or remove a systemd service that starts at boot. macOS: let the menu-bar app start without a password. |
| `version` | Print the version. |

`on` refuses to start routing when Start is blocked, for the reasons in [License](license.md#if-the-license-data-is-changed).

## macOS and Linux

`routengine` is the same engine. The command-line build has no window and no desktop libraries, so it runs on a bare server. The app packages contain the same commands.

```
curl -fsSL https://routengine.net/install.sh | sh     # or the .deb, .rpm and .tar.gz files on the release page
brew install champdev-me/tap/routengine               # Homebrew, macOS and Linux
routengine machine-code
routengine license add <key>
sudo routengine install                               # Linux: runs at boot as the systemd service "routengine"
```

**Files.** As root on Linux (`sudo`, or the service) they are in `/etc/routengine`. Otherwise they are in your config folder: `~/.config/routengine` on Linux, `~/Library/Application Support/routengine` on macOS. On macOS the app runs as root but uses your folder, so the app and the terminal agree. `ROUTENGINE_DIR` overrides all of this.

| File | What it is |
| ---- | ---------- |
| `config.json` | The rules. Same format as `rules.json` on Windows, and it wins when both exist. |
| `license.key` | Your Pro license key. |
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

The `nogui` tag leaves out the window and tray, which are not part of this repository. A program you build is not an official release: the licensing server does not know it, so do not install a Pro key on it.

**Limits.** On Linux a program of another user is only named when RoutEngine runs as root, which the service does. Anything that cannot be named is balanced and counted under "(unidentified)". The free edition has the same limits as the window app: 3 apps, each with a speed limit and a data cap.
