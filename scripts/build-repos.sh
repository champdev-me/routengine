#!/usr/bin/env bash
# Builds a signed apt repository and a signed dnf/zypper repository from .deb/.rpm files, as a static site that
# can be served from GitHub Pages. Needs apt-ftparchive (apt-utils), dpkg-deb, createrepo_c and gpg, with the
# signing key already in the keyring.
#
#   GPG_KEY_ID=<fingerprint> REPO_BASE_URL=https://champdev-me.github.io/routengine \
#       scripts/build-repos.sh PKG_DIR OUT_DIR
set -euo pipefail
PKGS=$(realpath "${1:?usage: build-repos.sh PKG_DIR OUT_DIR}")
mkdir -p "${2:?usage: build-repos.sh PKG_DIR OUT_DIR}"
OUT=$(realpath "$2")
KEY=${GPG_KEY_ID:?set GPG_KEY_ID to the signing key fingerprint}
BASE=${REPO_BASE_URL:-https://champdev-me.github.io/routengine}
sign() { gpg --batch --yes --pinentry-mode loopback --local-user "$KEY" "$@"; }

rm -rf "$OUT"/*
mkdir -p "$OUT/apt/pool/main" "$OUT/rpm"
cp "$PKGS"/*.deb "$OUT/apt/pool/main/"
cp "$PKGS"/*.rpm "$OUT/rpm/"

# ---- apt: dists/stable/main/binary-<arch>/Packages, Release, InRelease
cd "$OUT/apt"
archs=$(for f in pool/main/*.deb; do dpkg-deb -f "$f" Architecture; done | sort -u | tr '\n' ' ')
for arch in $archs; do
    d=dists/stable/main/binary-$arch
    mkdir -p "$d"
    apt-ftparchive --arch "$arch" packages pool > "$d/Packages"
    gzip -9kf "$d/Packages"
done
apt-ftparchive \
    -o APT::FTPArchive::Release::Origin=RoutEngine \
    -o APT::FTPArchive::Release::Label=RoutEngine \
    -o APT::FTPArchive::Release::Suite=stable \
    -o APT::FTPArchive::Release::Codename=stable \
    -o APT::FTPArchive::Release::Architectures="$archs" \
    -o APT::FTPArchive::Release::Components=main \
    -o APT::FTPArchive::Release::Description="RoutEngine: choose which network adapter each app uses" \
    release dists/stable > dists/stable/Release
sign --clearsign -o dists/stable/InRelease dists/stable/Release
sign --armor --detach-sign -o dists/stable/Release.gpg dists/stable/Release
gpg --export "$KEY" > key.gpg      # binary keyring, for signed-by=
gpg --armor --export "$KEY" > key.asc

# ---- dnf and zypper: repodata signed with repo_gpgcheck
cd "$OUT/rpm"
createrepo_c --quiet .
sign --armor --detach-sign -o repodata/repomd.xml.asc repodata/repomd.xml
gpg --armor --export "$KEY" > key.asc
cat > routengine.repo <<REPO
[routengine]
name=RoutEngine
baseurl=$BASE/rpm
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=$BASE/rpm/key.asc
REPO

# ---- a plain page at the root, so the site has something to show
cat > "$OUT/index.html" <<HTML
<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>RoutEngine packages</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:46rem;margin:2rem auto;padding:0 1rem}pre{background:#f3f4f6;padding:.8rem;overflow:auto;border-radius:6px}@media(prefers-color-scheme:dark){body{background:#0b1220;color:#e5e7eb}pre{background:#111827}a{color:#22d3ee}}</style>
<h1>RoutEngine packages</h1>
<p>Choose which network adapter each app uses. <a href="https://routengine.net">routengine.net</a> ·
<a href="https://github.com/champdev-me/routengine">docs on GitHub</a>.
Signing key fingerprint: <code>$KEY</code>.</p>
<p>Two packages: <code>routengine</code> (command line, for servers) and <code>routengine-desktop</code> (window and tray icon; it includes the command line). Install one, not both.</p>
<h2>Debian, Ubuntu, Mint, Pop!_OS, Raspberry Pi OS</h2>
<pre>curl -fsSL $BASE/apt/key.gpg | sudo tee /usr/share/keyrings/routengine.gpg &gt;/dev/null
echo "deb [signed-by=/usr/share/keyrings/routengine.gpg] $BASE/apt stable main" | sudo tee /etc/apt/sources.list.d/routengine.list
sudo apt update &amp;&amp; sudo apt install routengine</pre>
<h2>Fedora, RHEL, Rocky, Alma</h2>
<pre>sudo curl -fsSL -o /etc/yum.repos.d/routengine.repo $BASE/rpm/routengine.repo
sudo dnf install routengine</pre>
<h2>openSUSE</h2>
<pre>sudo zypper addrepo $BASE/rpm/routengine.repo
sudo zypper --gpg-auto-import-keys refresh
sudo zypper install routengine</pre>
<p>For the window app install <code>routengine-desktop</code> instead. Everything else: the <code>.tar.gz</code> for your CPU on the <a href="https://github.com/champdev-me/routengine/releases/latest">releases page</a>, or <code>brew install champdev-me/tap/routengine</code>.</p>
HTML
touch "$OUT/.nojekyll"
echo "built apt and dnf repositories in $OUT"
