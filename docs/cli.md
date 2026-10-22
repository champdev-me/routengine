# Command line

The installer puts `RoutEngine.exe` in `C:\Program Files\RoutEngine`. Most people only use the window. The commands below are for scripts and for checking status.

Run `RoutEngine.exe` with a command:

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
| `version` | Print the version. |

`on` refuses to start routing when Start is blocked, for the reasons in [License](license.md#if-the-license-data-is-changed).
