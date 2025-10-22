package update

import (
	"crypto/sha256"
	"testing"
)

func TestParseDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantAlg     string
		wantValue   string
		expectError bool
	}{
		{
			name:      "sha256 digest",
			input:     "sha256:abcdef123456",
			wantAlg:   "sha256",
			wantValue: "abcdef123456",
		},
		{
			name:        "missing value",
			input:       "",
			expectError: true,
		},
		{
			name:        "bad format",
			input:       "no-colon",
			expectError: true,
		},
		{
			name:        "empty algorithm",
			input:       " :abc",
			expectError: true,
		},
		{
			name:        "empty value",
			input:       "sha256:",
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, err := ParseDigest(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if d.Algorithm != tt.wantAlg {
				t.Fatalf("unexpected algorithm: want %s, got %s", tt.wantAlg, d.Algorithm)
			}

			if d.Value != tt.wantValue {
				t.Fatalf("unexpected value: want %s, got %s", tt.wantValue, d.Value)
			}
		})
	}
}

func TestHashForDigest(t *testing.T) {
	t.Parallel()

	hash, err := HashForDigest(Digest{Algorithm: "sha256"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash == nil {
		t.Fatalf("expected non-nil hash")
	}

	hash.Write([]byte("abc"))
	got := hash.Sum(nil)
	want := sha256.Sum256([]byte("abc"))
	if string(got) != string(want[:]) {
		t.Fatalf("hash mismatch: got %x want %x", got, want)
	}
}

func TestHashForDigestUnsupported(t *testing.T) {
	t.Parallel()

	if _, err := HashForDigest(Digest{Algorithm: "md5"}); err == nil {
		t.Fatalf("expected error for unsupported digest")
	}
}
