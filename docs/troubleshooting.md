# Troubleshooting

## Before you open an issue

1. Note your RoutEngine version: **Settings**, under **Updates**, or run `RoutEngine.exe version`.
2. Note your Windows version: press Win+R, type `winver`, press Enter.
3. Look at the log, below. Copy the last 30 lines or so.

Then [open a bug report](../../../issues/new?template=bug_report.yml).

## Where the log is

`%LocalAppData%\routengine\routengine.log`

Paste `%LocalAppData%\routengine` into the Explorer address bar to open the folder. The log may contain app names and adapter names. Read it before posting.

## Windows says the publisher is unknown

SmartScreen may warn that the program is from an unknown publisher. The installer is signed as RoutEngine, with a certificate that Windows does not know yet, so Windows has no reputation for it. Choose **More info**, then **Run anyway**. Download only from [routengine.net](https://routengine.net).

## Start is blocked

A red strip at the top says why, and the Start button is dimmed. The reasons:

| The strip says | What it means | What to do |
| -------------- | ------------- | ---------- |
| "The saved license key was changed or damaged." | The key file on this PC does not verify. | In **Settings**, press **Remove license** to use the free edition, or enter your key again. |
| "RoutEngine found that its saved license data was changed." | The licensing server's saved answer does not verify. | Connect to the internet so RoutEngine can check the license again. **Check again** in Settings does it at once. |
| "This copy of RoutEngine has been modified." | The licensing server does not know this program file. | Install RoutEngine again from [routengine.net](https://routengine.net). |

If it persists on a fresh, unmodified install, email sales@routengine.net with the text of the strip.

## Pro paused

Pro must verify online at least once every 7 days. Connect to the internet and press **Check again** in Settings. If a firewall blocks it, allow `RoutEngine.exe` to reach `routengine.net`.

If the message says the clock was set back, fix the PC's clock and connect to the internet. Start is not blocked in that case, but Pro stays paused until the check succeeds.

## "This license was issued for a different PC"

A key works on the PC whose machine code it was issued for. Check the code in Settings against the one you sent. To move a lifetime license, email sales@routengine.net with the new PC's machine code.

## An app is not using the adapter I chose

1. Check the app is in the **Applications** list, and that the status line says **Running**.
2. Look at the app's row: `using Ethernet 4` shows the adapter it is on now.
3. Open the **Usage** page. It lists every program that used the network, with the adapters each one used. If the traffic you expect comes from a program with a different name, add that program too.

## No adapters found

The app's panel says "No adapters found. Connect Wi-Fi, Ethernet or a phone." Connect one, and it appears in the list.

## The internet stopped after a crash

Run `RoutEngine.exe cleanup` from a command prompt opened as administrator. It removes leftover routes. Or start RoutEngine again and quit it from the tray.

## Updates

**Settings**, under **Updates**, shows your version and has **Check now**. When a newer version is ready, a strip at the top says so with an **Update now** button. **Install updates automatically** is on by default and installs only while routing is off. An update is checked against a signature before it runs.

If the strip says your license does not cover the new version, the new version is outside what your key covers. Email sales@routengine.net.

## Still stuck

[Search the issues](../../../issues?q=is%3Aissue), then [open one](../../../issues/new/choose). For anything about buying or your license, email sales@routengine.net.

## macOS says it cannot verify RoutEngine

The Mac app is not notarized by Apple. The first time, right-click RoutEngine in Applications, choose **Open**, then **Open** again. Or remove the flag in a terminal: `xattr -dr com.apple.quarantine /Applications/RoutEngine.app`. Installing with the `curl ... | sh` command from the README does not trigger the warning.

## No tray icon on Linux

GNOME needs the AppIndicator extension to show tray icons. The window works without it: start RoutEngine from the applications menu, or run `routengine app`.

## Linux: "needs root or CAP_NET_ADMIN"

The window app needs the network capability. The `routengine-desktop` package sets it. If you installed from a tarball: `sudo setcap cap_net_admin,cap_net_raw+ep $(which routengine)`. `sudo routengine run` works without it. If `/dev/net/tun` is missing: `sudo modprobe tun`.

