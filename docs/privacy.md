# Privacy

The legal text is the [privacy policy](https://routengine.net/privacy.html). This page is the plain version.

## What is never sent

RoutEngine does not look inside what your apps send or receive. It does not send your traffic, your app names, websites, files, usage numbers or adapter names anywhere. Routing happens on your PC.

## Free edition

Sends no data about you. It checks for updates by downloading a small public file (see below), and sends usage statistics only if you turn them on.

## Update check

About once a day RoutEngine downloads `https://routengine.net/update/latest.json`, a small file that names the newest version. It is signed, and a downloaded update is checked against that signature before it runs. The request is an ordinary web request and carries nothing about you or your PC.

**Install updates automatically** in Settings is on by default and installs only while routing is off. Turn it off to be asked instead.

## Pro license check

About once a day, and at least once every 7 days, a Pro install asks the licensing server to confirm the license. It sends:

- your license key,
- your machine code,
- the program version,
- a fingerprint (SHA-256) of the program file, so a modified copy can be recognised, and
- a random value that the answer must repeat.

The server answers with a signed lease. The check does not send your name or email address.

## Usage statistics (optional, off until you agree)

If you turn on **Send anonymous usage statistics** in Settings, about once a day RoutEngine sends: a random install ID, the version, free or Pro (with the license ID for Pro), your Windows version and architecture, how many apps you have rules for, how many adapters you have, and whether routing is on.

It never sends app names, websites, files, traffic, IP addresses or personal details. Turn it off at any time in Settings, or with `RoutEngine.exe stats off`.

## Tamper reports

When a Pro install finds that its saved license key or the licensing server's saved answer was edited or damaged, or that the PC clock was set back, it sends a short report: the kind of event, the license ID, the machine code, the program version and the program file fingerprint. Nothing else is in it.

## The server

The licensing server stores license records, the machine codes they are tied to, and when each last checked in. IP addresses are used in memory to limit abuse and are not stored or logged by the server.

## Contact

sales@routengine.net
