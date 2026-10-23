# Changelog

Newest first. Versions are numbered `major.minor.patch`. A lifetime Pro license covers every 1.x version.

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
