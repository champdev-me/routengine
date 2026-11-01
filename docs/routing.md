# Routing

Each app you add has its own rule. Open the app's panel and look at **Connect through**.

## The three modes

| Mode | What it does | Use it for |
| ---- | ------------ | ---------- |
| **Use all** | Spreads connections over every internet adapter. | Apps with many connections, when you want every link working. |
| **One adapter** | Always uses the adapter you pick. | An app that must stay on one link: a work app on Ethernet, a game on Wi-Fi. |
| **Several adapters** | Uses the adapters you tick, together. | Downloads or uploads that should use two links at once. |

## Combining adapters

With **Use all** or **Several adapters**, each new connection goes to the least busy adapter you chose. The speeds of many connections add up.

One single connection always stays on one adapter. So one big file from one server is not faster. Browsers, download managers and cloud uploads open many connections, and those do speed up.

## Fallback

When you pick **One adapter**, choose what happens if it goes down. Under **Fallback**:

- **Nothing**: its connections stop.
- **Default adapter**: its connections move to the adapter marked **DEFAULT** on the main window, until its own adapter is back. Click **Make default** on an adapter to change which one that is.
- **Choose adapters**: tried in the order shown.

## Last resort

For **One adapter** and **Several adapters** there is **Use any other adapter**. When it is on and none of the adapters you chose is available, the app uses any adapter that works. When it is off, the app's connections stop instead.

## VPN and Tailscale

A VPN or Tailscale connection appears in the adapter list like Wi-Fi or Ethernet. Choose it under **One adapter** to send an app through it, and leave your other apps alone.

RoutEngine is not a VPN itself. It does not encrypt your traffic.

## Fastest adapter (Pro)

With **Use all** or **Several adapters**, Pro adds **Use whichever adapter answers fastest**, which prefers the adapter that responds quickest.

## DNS per app (Pro)

Pro lets an app use DNS servers of its own, a preset or ones you type, instead of the system's. Set it in the app's panel. The default is **system**.

## What is not touched

Apps you did not add are not listed, but while RoutEngine is on their new connections are shared across all your adapters, like **Use all**. Press Stop, or quit, and everything returns to the system's own choice.
