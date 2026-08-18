package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateUpstreamBillingProbeConfigRejectsUnsafePaths(t *testing.T) {
	base := map[string]any{
		UpstreamBillingProbeAdapterExtraKey:  UpstreamBillingProbeAdapterCustomJSON,
		UpstreamBillingProbePathExtraKey:     "/api/pricing",
		UpstreamBillingProbeRatePathExtraKey: "data.rate",
	}

	for _, path := range []string{
		"https://other.example/pricing",
		"/api/../pricing",
		"/api/pricing?redirect=https://other.example",
		"/api/pricing#fragment",
		"/api/pricing\nnext",
	} {
		extra := mergeMap(nil, base)
		extra[UpstreamBillingProbePathExtraKey] = path
		require.Error(t, ValidateUpstreamBillingProbeConfig(extra), path)
	}

	for _, ratePath := range []string{"", "data..rate", "data.\nrate"} {
		extra := mergeMap(nil, base)
		extra[UpstreamBillingProbeRatePathExtraKey] = ratePath
		require.Error(t, ValidateUpstreamBillingProbeConfig(extra), ratePath)
	}
}

func TestParseCustomUpstreamBillingProbeResponseNewAPIGroupRatio(t *testing.T) {
	data, err := parseCustomUpstreamBillingProbeResponse(
		[]byte(`{"group_ratio":{"codex-special":0.42,"default":1}}`),
		upstreamBillingProbeAdapterConfig{
			Adapter:  UpstreamBillingProbeAdapterNewAPI,
			Path:     "/api/pricing",
			RatePath: "group_ratio",
			Group:    "codex-special",
		},
		time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC),
	)

	require.NoError(t, err)
	require.Equal(t, 0.42, data["resolved_rate_multiplier"])
	require.Equal(t, "new_api_pricing", data["probe_adapter"])
}

func TestParseCustomUpstreamBillingProbeResponseRejectsUnknownGroup(t *testing.T) {
	_, err := parseCustomUpstreamBillingProbeResponse(
		[]byte(`{"group_ratio":{"default":1}}`),
		upstreamBillingProbeAdapterConfig{
			Adapter:  UpstreamBillingProbeAdapterNewAPI,
			Path:     "/api/pricing",
			RatePath: "group_ratio",
			Group:    "missing",
		},
		time.Now(),
	)
	require.ErrorContains(t, err, "group")
}

func TestUpstreamBillingProbeCustomJSONUsesConfiguredRootPathAndSyncsRate(t *testing.T) {
	initialRate := 1.0
	account := &Account{
		ID:             901,
		Platform:       PlatformOpenAI,
		Type:           AccountTypeAPIKey,
		Status:         StatusActive,
		Concurrency:    1,
		RateMultiplier: &initialRate,
		Credentials: map[string]any{
			"api_key":  "sk-custom",
			"base_url": "https://upstream.example/v1",
		},
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey:    true,
			UpstreamBillingRateSyncEnabledExtraKey: true,
			UpstreamBillingProbeAdapterExtraKey:    UpstreamBillingProbeAdapterCustomJSON,
			UpstreamBillingProbePathExtraKey:       "/api/pricing",
			UpstreamBillingProbeRatePathExtraKey:   "data.rate",
		},
	}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":{"rate":0.37}}`)),
	}}
	svc := newUpstreamBillingProbeTestService(repo, upstream, &upstreamBillingProbeSettingRepo{})

	snapshot, err := svc.ProbeAccount(context.Background(), account.ID)

	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, snapshot.Status)
	require.Equal(t, 0.37, snapshot.Data["resolved_rate_multiplier"])
	require.NotNil(t, account.RateMultiplier)
	require.Equal(t, 0.37, *account.RateMultiplier)
	require.Equal(t, "https://upstream.example/api/pricing", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-custom", upstream.lastReq.Header.Get("Authorization"))
}
