package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/auth"
)

func TestIssueAndHashRoundTrip(t *testing.T) {
	raw, hash, err := auth.IssueToken()
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.NotEmpty(t, hash)
	require.NotEqual(t, raw, hash, "raw and hash must differ")
	require.Equal(t, hash, auth.HashToken(raw), "HashToken should be deterministic")
}

func TestIssueTokenIsURLSafeBase64(t *testing.T) {
	raw, _, err := auth.IssueToken()
	require.NoError(t, err)
	// URL-safe base64: only A-Z a-z 0-9 - _
	require.False(t, strings.ContainsAny(raw, "+/="))
}

func TestIssueTokenIsHighEntropy(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		raw, _, err := auth.IssueToken()
		require.NoError(t, err)
		_, dup := seen[raw]
		require.False(t, dup, "collision after %d issues", i)
		seen[raw] = struct{}{}
	}
}

func TestHashTokenStable(t *testing.T) {
	require.Equal(t, auth.HashToken("hello"), auth.HashToken("hello"))
	require.NotEqual(t, auth.HashToken("hello"), auth.HashToken("hello "))
}
