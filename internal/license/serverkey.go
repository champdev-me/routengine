package license

// ServerPublicKeyHex is the key the licensing server's answers are checked against; the server holds the
// private half (server-private.key). To use a different pair, run
// `go run ./scripts/licensegen keys -name server -dir <folder>` and paste the new public key here.
// Do it before the first release, like the license key pair.
const ServerPublicKeyHex = "d15cb9eac63f7491b83d7e859a61cf41d7036e3deb6e9d4137615162a3e4181b"
