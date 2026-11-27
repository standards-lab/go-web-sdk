package upload_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/standards-lab/go-web-sdk/upload"
)

const (
	emptySum   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	emptyField = "sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:"
)

func TestFormatDigest(t *testing.T) {
	s, err := upload.FormatDigest(emptySum)
	if err != nil || s != emptyField {
		t.Errorf("FormatDigest(%q) = %q, %v; want %q", emptySum, s, err, emptyField)
	}
}

func TestParseDigest(t *testing.T) {
	sum, ok := upload.ParseDigest(emptyField)
	if !ok || sum != emptySum {
		t.Errorf("ParseDigest(%q) = %q, %v; want %q", emptyField, sum, ok, emptySum)
	}
}

func TestDigestRoundTrip(t *testing.T) {
	for _, content := range []string{"", "a", "resumable upload chunk"} {
		raw := sha256.Sum256([]byte(content))
		sum := hex.EncodeToString(raw[:])
		field, err := upload.FormatDigest(sum)
		if err != nil {
			t.Fatalf("FormatDigest(%q): %v", sum, err)
		}
		back, ok := upload.ParseDigest(field)
		if !ok || back != sum {
			t.Errorf("ParseDigest(FormatDigest(%q)) = %q, %v", sum, back, ok)
		}
	}
}

func TestFormatDigestUppercaseHex(t *testing.T) {
	upper := "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855"
	s, err := upload.FormatDigest(upper)
	if err != nil || s != emptyField {
		t.Errorf("FormatDigest(%q) = %q, %v; want %q", upper, s, err, emptyField)
	}
}

func TestFormatDigestRefuses(t *testing.T) {
	for _, sum := range []string{"", "e3b0", emptySum + "00", "zz" + emptySum[2:]} {
		if s, err := upload.FormatDigest(sum); err == nil {
			t.Errorf("FormatDigest(%q) = %q, want an error", sum, s)
		}
	}
}

func TestParseDigestFields(t *testing.T) {
	cases := []struct {
		name, field string
		ok          bool
	}{
		{"other algorithms beside", "sha-512=:z4PhNX7vuL3xVChQ1m2AB9Yg5AULVxXcg/SpIdNs6c5H0NE8XYXysP+DGNKHfuwvY7kxvUdBeoGlODJ6+SfaPg==:, " + emptyField, true},
		{"parameters on the member", emptyField + ";note=1", true},
		{"repeated key keeps the last", "sha-256=:AAAA:, " + emptyField, true},
		{"absent", "sha-512=:AAAA:", false},
		{"not a byte sequence", "sha-256=1", false},
		{"wrong length", "sha-256=:AAAA:", false},
		{"malformed", "sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sum, ok := upload.ParseDigest(c.field)
			if ok != c.ok || (ok && sum != emptySum) {
				t.Errorf("ParseDigest(%q) = %q, %v; want ok %v", c.field, sum, ok, c.ok)
			}
		})
	}
}
