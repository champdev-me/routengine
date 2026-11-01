# Changelog

## 1.2.1

Windows, macOS, Linux and the command line. For macOS and Linux this also brings the 1.2.0 changes below.

- **The command line is free for any use**, work included, on any number of apps. It has no license key, no Pro edition and no online check, and every app can have a speed limit and a data cap. The `license`, `machine-code` and `limit` commands are now in the window apps only, with their Pro features (DNS per app, the fastest adapter, speed limits per adapter, usage history beyond today). The source here no longer contains the license code.
- Pro: the speed limit per adapter has a download and an upload limit of its own. An empty box is no limit that way. `limit <adapter> <down> [<up>]` from a terminal. Rules files from 1.2.0 load unchanged. See [Limits and caps](docs/limits-and-caps.md#speed-limit-per-adapter-pro).
- After an update RoutEngine starts again. Before, it could close at once when routing had been off, because it read the request to quit meant for the copy the installer replaced.
- An app that was already open when RoutEngine started is listed as open instead of "not running" (#1). Windows and Linux; macOS not yet.
- The window: panels slide in and out, pages and tabs move smoothly, buttons have icons, and Start, Stop, Check again, Check now and Export CSV show that they are working. Nothing moves while the window is idle, and the motion stops when Windows' "Show animations" is off.
- `quit` from a terminal waits until RoutEngine has closed.

## 1.2.0

Windows. The macOS, Linux and command-line downloads got these changes in 1.2.1.

- Pro: a speed limit per adapter. Cap a whole adapter, such as a phone tether, in Mbit/s each way, for all apps together. Set it in Settings under **Speed limit per adapter**, or with `RoutEngine.exe limit <adapter> <Mbit/s>`. In an app's panel you can also cap that one app on each adapter. See [Limits and caps](docs/limits-and-caps.md).
- The virtual adapter now takes an address that no other adapter on the PC uses, so it no longer clashes with one that already has its default address.
- Balancing no longer skips links that are healthy.
- The SOCKS5 proxy (`socks`) rejects malformed requests, and answers a command it does not support at once.

## 1.1.1

- macOS, Linux and the command-line version now tell you when a newer version exists. They never install anything by themselves.
- The update check says your operating system and CPU as well as the version, so installs can be counted without keeping IP addresses. See [Privacy](docs/privacy.md).
- `routengine run` now sends the anonymous usage statistics when you turned them on with `routengine stats on`, like the window apps do.
- The source of the command-line program is published in this repository under the PolyForm Noncommercial License 1.0.0.

## 1.1.0

- macOS and Linux: window app with a menu-bar or tray icon, the same features as Windows.
- A command-line version for Linux and macOS: `license add`, `machine-code`, a `config.json` rules file and a systemd service. It needs no desktop libraries.
- On macOS a rule for an `.app` covers every program inside it, so a browser's helper processes follow it.
- Free edition: all 3 apps can have a speed limit and a data cap (before, one app had a limit and one a cap).
- macOS and Linux have no self-update yet; install a new version over the old one.

## 1.0.0

The first release.

- Bind each app to an adapter: Wi-Fi, Ethernet, a phone tether, a VPN or Tailscale.
- Modes: use all, one adapter, several adapters together.
- Fallback to the default adapter or a list when an adapter goes down.
- Speed limit and data cap per app.
- Live speeds per adapter and per app, today's data, and a usage page for every app that used the network.
- Pro: unlimited apps, limits and caps on every app, DNS per app, fastest-adapter routing, usage history.
- Pro license check online about once a day, required at least every 7 days.
- Start is blocked, with a reason, when the saved license data was changed or the program file was modified. Such events are reported to the licensing server.
- Signed updates, installed automatically while routing is off unless you turn that off.
