package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestExportAccountExtraOmitsUpstreamPasswordCiphertext(t *testing.T) {
	extra := map[string]any{
		"custom": "keep",
		service.UpstreamAccountBalancePasswordExtraKey: "encrypted-secret",
	}

	exported := exportAccountExtra(extra)

	require.Equal(t, "keep", exported["custom"])
	require.NotContains(t, exported, service.UpstreamAccountBalancePasswordExtraKey)
	require.Contains(t, extra, service.UpstreamAccountBalancePasswordExtraKey, "export must not mutate account state")
}
