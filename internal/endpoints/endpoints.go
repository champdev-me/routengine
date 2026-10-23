// Package endpoints holds the addresses RoutEngine talks to. They are constants on purpose: no setting,
// file or environment variable can point the program somewhere else.
package endpoints

const (
	// Site is the product's website; the Get Pro buttons open it.
	Site = "https://routengine.net"

	// Ping receives the opt-in usage statistics (see SERVER.md).
	Ping = "https://api.routengine.net/v1/ping"

	// Check receives the license verification a Pro license requires (see SERVER.md).
	Check = "https://api.routengine.net/v1/check"

	// Tamper receives the short report a Pro install sends when it finds its license data was changed (see SERVER.md).
	Tamper = "https://api.routengine.net/v1/tamper"

	// Update is the signed description of the newest release (see SERVER.md). It is a plain file.
	Update = "https://routengine.net/update/latest.json"
)
