package update

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
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
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantAlg, d.Algorithm)
			require.Equal(t, tt.wantValue, d.Value)
		})
	}
}

func TestHashForDigest(t *testing.T) {
	t.Parallel()

	hash, err := HashForDigest(Digest{Algorithm: "sha256"})
	require.NoError(t, err)
	require.NotNil(t, hash)

	_, err = hash.Write([]byte("abc"))
	require.NoError(t, err)
	got := hash.Sum(nil)
	want := sha256.Sum256([]byte("abc"))
	require.Equal(t, want[:], got)
}

func TestHashForDigestUnsupported(t *testing.T) {
	t.Parallel()

	_, err := HashForDigest(Digest{Algorithm: "md5"})
	require.Error(t, err)
}
