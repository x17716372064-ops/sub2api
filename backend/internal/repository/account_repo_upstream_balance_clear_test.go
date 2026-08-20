package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAccountBalanceExtraClearKeysOnlySelectNilFields(t *testing.T) {
	keys := upstreamAccountBalanceExtraClearKeys(map[string]any{
		service.UpstreamAccountBalanceConfigExtraKey:   nil,
		service.UpstreamAccountBalancePasswordExtraKey: "ciphertext",
		service.UpstreamAccountBalanceSnapshotExtraKey: nil,
	})

	require.Equal(t, []string{
		service.UpstreamAccountBalanceConfigExtraKey,
		service.UpstreamAccountBalanceSnapshotExtraKey,
	}, keys)
}

func TestUpstreamAccountBalanceExtraIsSchedulerNeutral(t *testing.T) {
	require.True(t, isSchedulerNeutralExtraKey(service.UpstreamAccountBalanceConfigExtraKey))
	require.True(t, isSchedulerNeutralExtraKey(service.UpstreamAccountBalancePasswordExtraKey))
	require.True(t, isSchedulerNeutralExtraKey(service.UpstreamAccountBalanceSnapshotExtraKey))
}
