# Third-party notices

RoutEngine links the following libraries, all under permissive licenses (license texts are in each
project's repository and in the Go module cache):

| Component | License | Use |
|---|---|---|
| [tun2socks](https://github.com/xjasonlyu/tun2socks) | MIT | TUN device + userland TCP/IP stack glue |
| [gVisor](https://github.com/google/gvisor) (netstack) | Apache-2.0 | userland TCP/IP stack |
| [wireguard-go](https://git.zx2c4.com/wireguard-go) / [wintun Go bindings](https://git.zx2c4.com/wintun-go) | MIT | Windows TUN device access |
| [fyne.io/systray](https://github.com/fyne-io/systray) | Apache-2.0 | system tray icon |
| [vishvananda/netlink](https://github.com/vishvananda/netlink) | Apache-2.0 | Linux TUN addresses and routes, default-route discovery |
| golang.org/x/sys, golang.org/x/net, golang.org/x/time, golang.org/x/crypto | BSD-3-Clause | Windows and Linux system APIs, support code |
| Other transitive modules (see `go.mod` / `go.sum`) | MIT / Apache-2.0 / BSD | |

## Portions of RoutEngine's own source

Parts of RoutEngine's source code, including contributions by other developers, come from a project
that was published under the MIT License. The notice that license requires follows.

```
MIT License

Copyright (c) 2026 Rohit Mahto

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Wintun

The installer includes `wintun.dll`, the prebuilt, signed driver from
<https://www.wintun.net/>, unmodified, under the Prebuilt Binaries License. Copyright (C) 2018-2021
WireGuard LLC. The license text is shipped next to it as `wintun-LICENSE.txt`. The DLL is not stored
in this repository: `scripts/fetch-wintun.ps1` downloads it and verifies its SHA-256 and its
WireGuard LLC code signature.
