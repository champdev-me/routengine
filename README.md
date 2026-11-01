<p align="center">
  <img src="docs/images/icon.png" width="128" alt="RoutEngine icon">
</p>

<h1 align="center">RoutEngine</h1>

<p align="center">
  <strong>Bind every app to an adapter, a speed limit, a data cap.</strong><br>
  Choose which network connection each app uses: Wi-Fi, Ethernet, a phone tether or a VPN. Fail over when a link drops, combine links for speed, and limit speed or data per app.
</p>

<h1 align="center">This repository is for the command line.</h1>
<h2 align="center">For the window app (GUI), download it from <a href="https://routengine.net/#download">routengine.net</a>.</h2>
<p align="center">Windows, macOS and Linux. Not from GitHub.</p>

<p align="center">
  <a href="https://routengine.net/#download">Get the window app</a> ·
  <a href="#install">Install the command line</a> ·
  <a href="#command-line">Command line</a> ·
  <a href="#free-and-pro">Free and Pro</a> ·
  <a href="docs/README.md">Docs</a> ·
  <a href="../../issues/new/choose">Report a problem</a>
</p>

<p align="center">
  <a href="https://github.com/champdev-me/routengine/releases/latest"><img src="https://img.shields.io/github/v/release/champdev-me/routengine" alt="Latest release"></a>
  <a href="https://github.com/champdev-me/routengine/releases"><img src="https://img.shields.io/github/downloads/champdev-me/routengine/total" alt="Downloads"></a>
  <img src="https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-blue" alt="Windows, macOS and Linux">
  <img src="https://img.shields.io/badge/CLI-Linux%20%7C%20macOS-informational" alt="Command line for Linux and macOS">
  <a href="LICENSE"><img src="https://img.shields.io/badge/CLI%20source-PolyForm%20Noncommercial-blue" alt="Command-line source: PolyForm Noncommercial"></a>
  <a href="https://github.com/champdev-me/routengine/actions/workflows/ci.yml"><img src="https://github.com/champdev-me/routengine/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

<p align="center">
  <img src="docs/images/overview.png" alt="RoutEngine showing two adapters and three apps, each routed its own way" width="860">
</p>

RoutEngine decides which of your connections each app uses. Send your browser over Ethernet, your downloads over a phone tether, your work app through a VPN. It runs on **Windows, macOS and Linux**, and there is a **command-line version** for servers and terminals. It is free for personal use.

It is not a VPN. It does not encrypt or hide your traffic. It only chooses which of your own connections an app uses.

**Press Start and it works.** You do not have to add an app first: every app's new connections are shared across your adapters. Add an app when you want it to have its own rule, a speed limit or a data cap. Press Stop, or quit, and everything returns to the system's own routing at once.

| Windows | macOS | Linux |
|---|---|---|
| ![Windows](docs/images/overview.png) | ![macOS](docs/images/overview-mac.png) | ![Linux](docs/images/overview-linux.png) |

## What it does

- **Pick the adapter for each app.** Wi-Fi, Ethernet, a phone tether, a VPN or Tailscale.
- **Fail over.** If an app's adapter goes down, its connections move to your default adapter or to a list you choose, in order.
- **Combine adapters for speed.** Each new connection goes to the least busy adapter you chose, so browsers, download managers and cloud uploads add the speeds together.
- **Speed limit and data cap per app.** A download and upload limit, or a monthly or daily cap. You are warned at 80%.
- **Speed limit per adapter (Pro, window apps).** Cap a whole adapter, such as a phone tether, with a download and an upload limit of its own.
- **See what uses your network.** Live speed per adapter and per app, today's data, and a usage page for every app that touched the network.
- **Clean exit.** The virtual adapter and its routes are removed when you quit. Nothing is left behind.

<p align="center">
  <img src="docs/images/app-panel.png" alt="The panel for one app: connect through one adapter, with a fallback" width="640">
</p>

## Install

This installs the **command-line version**, for a terminal or a server. **For the window app (GUI), download it from [routengine.net](https://routengine.net/#download)**, not from GitHub.

**macOS and Linux**: paste this into a terminal.

```
curl -fsSL https://routengine.net/install.sh | sh
```

**Homebrew** (macOS and Linux):

```
brew install champdev-me/tap/routengine
```

**Linux package managers** (updates come with your system): a signed apt, dnf and zypper repository. Debian, Ubuntu and similar:

```
curl -fsSL https://champdev-me.github.io/routengine/apt/key.gpg | sudo tee /usr/share/keyrings/routengine.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/routengine.gpg] https://champdev-me.github.io/routengine/apt stable main" | sudo tee /etc/apt/sources.list.d/routengine.list
sudo apt update && sudo apt install routengine
```

Fedora, RHEL, Rocky, Alma:

```
sudo curl -fsSL -o /etc/yum.repos.d/routengine.repo https://champdev-me.github.io/routengine/rpm/routengine.repo
sudo dnf install routengine
```

openSUSE and the repository's signing key: [https://champdev-me.github.io/routengine](https://champdev-me.github.io/routengine/).

Routing needs administrator rights, because RoutEngine creates a virtual network adapter. `routengine run` asks for them, and on Linux `sudo routengine install` sets it up as a service that starts at boot.

## Command line

The same engine, with no window and no desktop libraries. It runs on a bare Linux server.

**Free for any use, work included, on any number of apps.** No key, no account, no Pro edition.

```
sudo routengine install            # Linux: a systemd service that starts at boot
routengine status                  # on, off, list, quit work too
```

Rules go in `/etc/routengine/config.json` (`~/.config/routengine/config.json` when you are not root):

```json
{
  "rules": [
    { "exe": "qbittorrent", "adapter": "eth1", "failover": ["default"] },
    { "exe": "curl", "balance": ["eth0", "wlan0"] }
  ]
}
```

On macOS a name ending in `.app` covers everything inside that app, so a browser's helper processes follow it. Full list of commands and options: [Command line](docs/cli.md).

## Free and Pro

The window apps have a free edition and Pro. The command line is free for any use and has no Pro edition.

|                                   | Window app, Free              | Window app, Pro        | Command line           |
| --------------------------------- | ----------------------------- | ---------------------- | ---------------------- |
| Use                               | Personal, non-commercial      | Work and business too  | Any use, work included |
| Apps                              | Up to 3                       | Unlimited              | Unlimited              |
| Per-app routing and fallback      | Yes                           | Yes                    | Yes                    |
| VPN and Tailscale adapters        | Yes                           | Yes                    | Yes                    |
| Combine adapters for speed        | Yes                           | Yes                    | Yes                    |
| Live speeds and today's data      | Yes                           | Yes                    | Yes                    |
| Speed limit                       | On each of your 3 apps        | On every app           | On every app           |
| Data cap                          | On each of your 3 apps        | On every app           | On every app           |
| Speed limit per adapter           | No                            | Yes                    | No                     |
| DNS per app                       | No                            | Yes                    | No                     |
| Fastest-adapter routing           | No                            | Yes                    | No                     |
| Usage history beyond today        | No                            | Yes                    | No                     |

Pro is ₹1,199 per PC per year, or ₹2,499 once for every 1.x version. Prices are in Indian rupees. Bulk keys are available for many PCs. Current prices are always on [routengine.net](https://routengine.net). The command line needs no key.

### How to buy

1. Open **Settings** in RoutEngine and press **Copy** next to **This PC**. That is your machine code.
2. Email **sales@routengine.net** with the plan you want and your machine code.
3. We reply with how to pay. After you pay, send us a screenshot of the payment.
4. We check it and email your license key within 24 hours, usually sooner.
5. Paste the key into **Settings**, under **License key**, and press **Activate**.

Details, including moving a license to a new PC, are in [License](docs/license.md).

## Source code

The source of the **command-line program** is in this repository (`cmd/` and `internal/`), under the [PolyForm Noncommercial License 1.0.0](LICENSE): you may read it, build it, change it and use it for noncommercial purposes, such as personal use, hobby projects, research and education. This is source-available, not an OSI open-source license. The builds we publish (the install script, Homebrew, the apt and dnf repositories and the release files) are free for any use, work included. The window apps are closed source.

```
git clone https://github.com/champdev-me/routengine && cd routengine
go build -tags nogui -o routengine ./cmd/routengine     # Linux and macOS; CGO_ENABLED=0 gives a static program on Linux
```


## Documentation

| Page | What is in it |
| ---- | ------------- |
| [Getting started](docs/getting-started.md) | Install, add an app, start and stop |
| [Routing](docs/routing.md) | Use all, one adapter, several adapters, fallback, VPN |
| [Limits and caps](docs/limits-and-caps.md) | Speed limits, data caps, usage history |
| [License](docs/license.md) | Free and Pro, machine code, the weekly check, moving PCs |
| [Privacy](docs/privacy.md) | What is sent, when, and how to turn it off |
| [Command line](docs/cli.md) | Commands on Windows, macOS and Linux, `config.json`, the service, building from source |
| [Troubleshooting](docs/troubleshooting.md) | Common problems, messages and where the log is |

## Report a problem or ask for a feature

This repository's [Issues](../../issues) are how RoutEngine problems and requests are tracked.

- **Found a bug?** [Open a bug report](../../issues/new?template=bug_report.yml). The form asks for your version and system, and what you expected.
- **Want something added?** [Open a feature request](../../issues/new?template=feature_request.yml).
- **Question about buying or your license?** Email sales@routengine.net. Do not post license keys or machine codes in public issues.
- **Security problem?** Read [SECURITY.md](SECURITY.md). Do not use a public issue.

## Privacy

The free edition and the command line send no data about you beyond a daily update check (the version, your system and CPU; installs are counted without keeping IP addresses). A Pro license is checked online about once a day and must be verified at least once a week. Usage statistics are off unless you turn them on, and are anonymous. Full details are in [Privacy](docs/privacy.md).

## License

- The command-line source code in this repository: [PolyForm Noncommercial 1.0.0](LICENSE). Third-party components are listed in [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).
- The programs you download from routengine.net (the window apps and the command-line build): the [end-user license agreement](https://routengine.net/eula.txt). Its section 1.6 makes the command-line build free for any use. The window apps are closed source.
- The documentation and screenshots: copyright RoutEngine, all rights reserved, except that you may quote and link to them when writing about RoutEngine.

<p align="center">
  <sub>Want something free and open source? <a href="https://github.com/rohit267/net-merge"><strong>net-merge</strong></a> combines all your connections into one. Same developer.</sub>
</p>
