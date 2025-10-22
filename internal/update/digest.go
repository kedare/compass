package update

import (
	"crypto"
	"fmt"
	"hash"
	"strings"
)

// Digest represents a parsed <algorithm>:<hex> GitHub release asset digest.
type Digest struct {
	Algorithm string
	Value     string
}

// ErrDigestMissing indicates that a release asset does not expose a digest.
var ErrDigestMissing = fmt.Errorf("asset digest is missing")

// ErrDigestUnsupported signals that the digest algorithm is not supported locally.
var ErrDigestUnsupported = fmt.Errorf("asset digest algorithm is not supported")

// ParseDigest splits a GitHub asset digest string into algorithm and hex value.
func ParseDigest(raw string) (Digest, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Digest{}, ErrDigestMissing
	}

	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return Digest{}, fmt.Errorf("invalid digest format %q", raw)
	}

	d := Digest{
		Algorithm: strings.ToLower(strings.TrimSpace(parts[0])),
		Value:     strings.TrimSpace(parts[1]),
	}

	if d.Algorithm == "" || d.Value == "" {
		return Digest{}, fmt.Errorf("invalid digest format %q", raw)
	}

	return d, nil
}

// HashForDigest returns a hash.Hash constructor that matches the provided digest.
func HashForDigest(d Digest) (hash.Hash, error) {
	switch d.Algorithm {
	case "sha256":
		return crypto.SHA256.New(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrDigestUnsupported, d.Algorithm)
	}
}
