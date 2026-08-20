package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type upstreamAccountBalanceRepoStub struct {
	AccountRepository
	account *Account
	updates []map[string]any
}

func (r *upstreamAccountBalanceRepoStub) GetByID(_ context.Context, _ int64) (*Account, error) {
	return r.account, nil
}

func (r *upstreamAccountBalanceRepoStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.account.Extra == nil {
		r.account.Extra = make(map[string]any)
	}
	cloned := make(map[string]any, len(updates))
	for key, value := range updates {
		cloned[key] = value
		if value == nil {
			delete(r.account.Extra, key)
		} else {
			r.account.Extra[key] = value
		}
	}
	r.updates = append(r.updates, cloned)
	return nil
}

type upstreamAccountBalanceHTTPStub struct {
	HTTPUpstream
	responses []upstreamAccountBalanceHTTPResponse
	requests  []*http.Request
}

type upstreamAccountBalanceHTTPResponse struct {
	status  int
	payload string
}

func (h *upstreamAccountBalanceHTTPStub) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	h.requests = append(h.requests, req)
	if len(h.responses) == 0 {
		return nil, io.EOF
	}
	response := h.responses[0]
	h.responses = h.responses[1:]
	return &http.Response{
		StatusCode: response.status,
		Body:       io.NopCloser(strings.NewReader(response.payload)),
		Header:     make(http.Header),
	}, nil
}

type upstreamAccountBalanceEncryptorStub struct{}

func (upstreamAccountBalanceEncryptorStub) Encrypt(plaintext string) (string, error) {
	return "cipher:" + plaintext, nil
}

func (upstreamAccountBalanceEncryptorStub) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "cipher:"), nil
}

func newUpstreamAccountBalanceTestService(
	account *Account,
	httpUpstream HTTPUpstream,
) (*UpstreamAccountBalanceService, *upstreamAccountBalanceRepoStub) {
	repo := &upstreamAccountBalanceRepoStub{account: account}
	return NewUpstreamAccountBalanceService(repo, httpUpstream, upstreamAccountBalanceEncryptorStub{}, nil), repo
}

func configuredUpstreamBalanceAccount() *Account {
	return &Account{
		ID: 7,
		Extra: map[string]any{
			UpstreamAccountBalanceConfigExtraKey: map[string]any{
				"provider": UpstreamAccountBalanceProviderNewAPI,
				"website":  "https://upstream.example.test",
				"email":    "owner@example.test",
			},
			UpstreamAccountBalancePasswordExtraKey: "cipher:secret",
		},
	}
}

func TestUpstreamAccountBalanceRefreshParsesTokenAndQuota(t *testing.T) {
	httpStub := &upstreamAccountBalanceHTTPStub{responses: []upstreamAccountBalanceHTTPResponse{
		{status: http.StatusOK, payload: `{"data":{"token":"token-123"}}`},
		{status: http.StatusOK, payload: `{"data":{"quota":"1000","used_quota":250}}`},
	}}
	svc, _ := newUpstreamAccountBalanceTestService(configuredUpstreamBalanceAccount(), httpStub)

	state, err := svc.Refresh(context.Background(), 7)

	require.NoError(t, err)
	require.NotNil(t, state.Snapshot)
	require.Equal(t, UpstreamAccountBalanceStatusOK, state.Snapshot.Status)
	require.NotNil(t, state.Snapshot.RawQuota)
	require.Equal(t, 1000.0, *state.Snapshot.RawQuota)
	require.NotNil(t, state.Snapshot.UsedQuota)
	require.Equal(t, 250.0, *state.Snapshot.UsedQuota)
	require.NotNil(t, state.Snapshot.RemainingQuota)
	require.Equal(t, 750.0, *state.Snapshot.RemainingQuota)
	require.Len(t, httpStub.requests, 2)
	require.Equal(t, "/api/user/login", httpStub.requests[0].URL.Path)
	require.Equal(t, "/api/user/self", httpStub.requests[1].URL.Path)
	require.Equal(t, "Bearer token-123", httpStub.requests[1].Header.Get("Authorization"))
}

func TestUpstreamAccountBalanceRefreshLoginFailureDoesNotReportZero(t *testing.T) {
	httpStub := &upstreamAccountBalanceHTTPStub{responses: []upstreamAccountBalanceHTTPResponse{
		{status: http.StatusUnauthorized, payload: `{"message":"invalid credentials"}`},
	}}
	svc, _ := newUpstreamAccountBalanceTestService(configuredUpstreamBalanceAccount(), httpStub)

	state, err := svc.Refresh(context.Background(), 7)

	require.NoError(t, err)
	require.NotNil(t, state.Snapshot)
	require.Equal(t, UpstreamAccountBalanceStatusFailed, state.Snapshot.Status)
	require.Equal(t, "login_rejected", state.Snapshot.LastError)
	require.Nil(t, state.Snapshot.Balance)
	require.Nil(t, state.Snapshot.RawQuota)
	require.Nil(t, state.Snapshot.RemainingQuota)
}

func TestUpstreamAccountBalanceStateDoesNotExposePassword(t *testing.T) {
	account := configuredUpstreamBalanceAccount()
	svc, _ := newUpstreamAccountBalanceTestService(account, &upstreamAccountBalanceHTTPStub{})

	state, err := svc.GetState(context.Background(), account.ID)

	require.NoError(t, err)
	require.True(t, state.Configured)
	require.True(t, state.PasswordConfigured)
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "cipher:secret")
	require.NotContains(t, string(encoded), `"password":"`)
}

func TestUpstreamAccountBalanceSaveConfigEncryptsPasswordAndCanClear(t *testing.T) {
	account := &Account{ID: 9}
	svc, repo := newUpstreamAccountBalanceTestService(account, &upstreamAccountBalanceHTTPStub{})

	state, err := svc.SaveConfig(context.Background(), account.ID, UpstreamAccountBalanceSaveRequest{
		Provider: UpstreamAccountBalanceProviderNewAPI,
		Website:  "https://upstream.example.test/",
		Email:    "owner@example.test",
		Password: "secret",
	})

	require.NoError(t, err)
	require.True(t, state.Configured)
	require.True(t, state.PasswordConfigured)
	require.NotEqual(t, "secret", repo.account.Extra[UpstreamAccountBalancePasswordExtraKey])
	require.Equal(t, "cipher:secret", repo.account.Extra[UpstreamAccountBalancePasswordExtraKey])

	cleared, err := svc.SaveConfig(context.Background(), account.ID, UpstreamAccountBalanceSaveRequest{ClearPassword: true})

	require.NoError(t, err)
	require.False(t, cleared.Configured)
	require.False(t, cleared.PasswordConfigured)
	require.NotContains(t, repo.account.Extra, UpstreamAccountBalanceConfigExtraKey)
	require.NotContains(t, repo.account.Extra, UpstreamAccountBalancePasswordExtraKey)
}
