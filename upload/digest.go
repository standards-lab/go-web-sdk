package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// See https://www.rfc-editor.org/rfc/rfc9530

const digestKey = "sha-256"

// FormatDigest returns the Repr-Digest or Content-Digest field value that
// carries sum, a SHA-256 written as hex, as its sha-256 member: for the
// empty input's sum, sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:.
// A sum that is not 64 hex digits returns an error.
func FormatDigest(sum string) (string, error) {
	b, err := hex.DecodeString(sum)
	if err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("upload: digest %q is not a hex SHA-256", sum)
	}
	return FormatDictionary(Dictionary{{Key: digestKey, Value: b}})
}

// ParseDigest returns the SHA-256 that a Repr-Digest or Content-Digest
// field value carries as its sha-256 member, in lowercase hex, and reports
// whether it carries one. A malformed field, or one whose sha-256 member is
// absent, not a Byte Sequence, or not 32 bytes, reports false; members for
// other algorithms are ignored.
func ParseDigest(field string) (string, bool) {
	d, ok := ParseDictionary(field)
	if !ok {
		return "", false
	}
	for _, m := range d {
		if m.Key != digestKey {
			continue
		}
		b, ok := m.Value.([]byte)
		if !ok || len(b) != sha256.Size {
			return "", false
		}
		return hex.EncodeToString(b), true
	}
	return "", false
}
