package license

// PublicKeyHex is the key licenses are checked against; its private half is held by the seller only.
// To use a different key pair, run `go run ./scripts/licensegen keys` and paste the new public key
// here. Licenses issued under the old key stop working, so do it before the first release.
const PublicKeyHex = "b5aea81d5191d3870f3342b778596509d897d5b7b65ab39284fb33715e3d9c2a"
