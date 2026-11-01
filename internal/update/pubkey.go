package update

// PublicKeyHex is the key releases are checked against; its private half (update-private.key) is held by
// the seller only and signs each release's manifest (`licensegen release`). To use a different pair, run
// `go run ./scripts/licensegen keys -name update -dir <folder>` and paste the new public key here. Do it
// before the first release, like the license key pair.
const PublicKeyHex = "7cf46c35e599373c722a4df69c65e2a8e4161fa060aacdd500ee3472f1bb6a71"
