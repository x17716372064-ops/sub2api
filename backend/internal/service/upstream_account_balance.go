package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// UpstreamAccountBalance* keys are kept in accounts.extra so this feature can
// be added without a database migration. The password is always encrypted;
// it is never returned in an account DTO or stored in the snapshot.
const (
	UpstreamAccountBalanceConfigExtraKey   = "upstream_account_balance_config"
	UpstreamAccountBalancePasswordExtraKey = "upstream_account_balance_password"
	UpstreamAccountBalanceSnapshotExtraKey = "upstream_account_balance_snapshot"

	UpstreamAccountBalanceProviderNewAPI = "new_api"

	upstreamAccountBalanceRequestTimeout = 15 * time.Second
	upstreamAccountBalanceMaxBodyBytes   = 512 * 1024
)

var (
	ErrUpstreamAccountBalanceUnavailable = infraerrors.ServiceUnavailable(
		"UPSTREAM_ACCOUNT_BALANCE_UNAVAILABLE", "upstream account balance query is unavailable",
	)
	ErrUpstreamAccountBalanceConfigInvalid = infraerrors.BadRequest(
		"UPSTREAM_ACCOUNT_BALANCE_CONFIG_INVALID", "upstream account balance configuration is invalid",
	)
	ErrUpstreamAccountBalancePasswordRequired = infraerrors.BadRequest(
		"UPSTREAM_ACCOUNT_BALANCE_PASSWORD_REQUIRED", "upstream account password is required",
	)
	ErrUpstreamAccountBalanceEncryptionKey = infraerrors.BadRequest(
		"UPSTREAM_ACCOUNT_BALANCE_ENCRYPTION_KEY_NOT_CONFIGURED", "cannot store upstream account password without a fixed encryption key",
	)
)

const (
	UpstreamAccountBalanceStatusOK          = "ok"
	UpstreamAccountBalanceStatusFailed      = "failed"
	UpstreamAccountBalanceStatusUnsupported = "unsupported"
)

type UpstreamAccountBalanceConfig struct {
	Provider   string `json:"provider"`
	Website    string `json:"website"`
	Email      string `json:"email"`
	Configured bool   `json:"configured"`
}

type UpstreamAccountBalanceSnapshot struct {
	Status         string     `json:"status"`
	Balance        *float64   `json:"balance,omitempty"`
	RawQuota       *float64   `json:"raw_quota,omitempty"`
	UsedQuota      *float64   `json:"used_quota,omitempty"`
	RemainingQuota *float64   `json:"remaining_quota,omitempty"`
	Unit           string     `json:"unit,omitempty"`
	RetrievedAt    *time.Time `json:"retrieved_at,omitempty"`
	LastAttemptAt  time.Time  `json:"last_attempt_at"`
	HTTPStatus     int        `json:"http_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type UpstreamAccountBalanceState struct {
	AccountID          int64                           `json:"account_id"`
	Configured         bool                            `json:"configured"`
	PasswordConfigured bool                            `json:"password_configured"`
	Config             *UpstreamAccountBalanceConfig   `json:"config,omitempty"`
	Snapshot           *UpstreamAccountBalanceSnapshot `json:"snapshot,omitempty"`
}

type UpstreamAccountBalanceSaveRequest struct {
	Provider      string `json:"provider"`
	Website       string `json:"website"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	ClearPassword bool   `json:"clear_password"`
}

// UpstreamAccountBalanceService handles official-site login probes for an
// account. The first adapter intentionally targets New API-compatible sites;
// provider-specific adapters can be added without changing the admin API.
type UpstreamAccountBalanceService struct {
	accountRepo  AccountRepository
	httpUpstream HTTPUpstream
	encryptor    SecretEncryptor
	cfg          *config.Config
}

func NewUpstreamAccountBalanceService(
	accountRepo AccountRepository,
	httpUpstream HTTPUpstream,
	encryptor SecretEncryptor,
	cfg *config.Config,
) *UpstreamAccountBalanceService {
	return &UpstreamAccountBalanceService{
		accountRepo:  accountRepo,
		httpUpstream: httpUpstream,
		encryptor:    encryptor,
		cfg:          cfg,
	}
}

func ProvideUpstreamAccountBalanceService(
	accountRepo AccountRepository,
	httpUpstream HTTPUpstream,
	encryptor SecretEncryptor,
	cfg *config.Config,
) *UpstreamAccountBalanceService {
	return NewUpstreamAccountBalanceService(accountRepo, httpUpstream, encryptor, cfg)
}

func (s *UpstreamAccountBalanceService) GetState(ctx context.Context, accountID int64) (*UpstreamAccountBalanceState, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrUpstreamAccountBalanceUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return s.stateFromAccount(account), nil
}

func (s *UpstreamAccountBalanceService) SaveConfig(
	ctx context.Context,
	accountID int64,
	req UpstreamAccountBalanceSaveRequest,
) (*UpstreamAccountBalanceState, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrUpstreamAccountBalanceUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.IsCredentialShadow() {
		return nil, ErrUpstreamAccountBalanceConfigInvalid.WithMetadata(map[string]string{"reason": "shadow_account"})
	}

	website := strings.TrimRight(strings.TrimSpace(req.Website), "/")
	if website == "" {
		// Empty website explicitly clears the feature.
		if err := s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
			UpstreamAccountBalanceConfigExtraKey:   nil,
			UpstreamAccountBalancePasswordExtraKey: nil,
			UpstreamAccountBalanceSnapshotExtraKey: nil,
		}); err != nil {
			return nil, err
		}
		account.Extra = cloneMap(account.Extra)
		delete(account.Extra, UpstreamAccountBalanceConfigExtraKey)
		delete(account.Extra, UpstreamAccountBalancePasswordExtraKey)
		delete(account.Extra, UpstreamAccountBalanceSnapshotExtraKey)
		return s.stateFromAccount(account), nil
	}

	normalizedWebsite, err := validateUpstreamAccountBalanceWebsite(s.cfg, website)
	if err != nil {
		return nil, ErrUpstreamAccountBalanceConfigInvalid.WithMetadata(map[string]string{"reason": "website_url"}).WithCause(err)
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = UpstreamAccountBalanceProviderNewAPI
	}
	if provider != UpstreamAccountBalanceProviderNewAPI {
		return nil, ErrUpstreamAccountBalanceConfigInvalid.WithMetadata(map[string]string{"reason": "provider"})
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || len([]rune(email)) > 320 {
		return nil, ErrUpstreamAccountBalanceConfigInvalid.WithMetadata(map[string]string{"reason": "email"})
	}

	passwordCiphertext, passwordConfigured := upstreamAccountBalancePassword(account)
	if strings.TrimSpace(req.Password) != "" {
		if s.encryptor == nil {
			return nil, ErrUpstreamAccountBalanceEncryptionKey
		}
		passwordCiphertext, err = s.encryptor.Encrypt(req.Password)
		if err != nil {
			return nil, fmt.Errorf("encrypt upstream account password: %w", err)
		}
		passwordConfigured = true
	} else if req.ClearPassword {
		passwordCiphertext = ""
		passwordConfigured = false
	}
	if !passwordConfigured {
		return nil, ErrUpstreamAccountBalancePasswordRequired
	}

	configValue := map[string]any{
		"provider": provider,
		"website":  normalizedWebsite,
		"email":    email,
	}
	updates := map[string]any{
		UpstreamAccountBalanceConfigExtraKey: configValue,
	}
	if passwordCiphertext != "" {
		updates[UpstreamAccountBalancePasswordExtraKey] = passwordCiphertext
	} else {
		updates[UpstreamAccountBalancePasswordExtraKey] = nil
	}
	if err := s.accountRepo.UpdateExtra(ctx, accountID, updates); err != nil {
		return nil, err
	}
	account.Extra = cloneMap(account.Extra)
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[UpstreamAccountBalanceConfigExtraKey] = configValue
	if passwordCiphertext != "" {
		account.Extra[UpstreamAccountBalancePasswordExtraKey] = passwordCiphertext
	} else {
		delete(account.Extra, UpstreamAccountBalancePasswordExtraKey)
	}
	return s.stateFromAccount(account), nil
}

func (s *UpstreamAccountBalanceService) Refresh(ctx context.Context, accountID int64) (*UpstreamAccountBalanceState, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, ErrUpstreamAccountBalanceUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	configValue := upstreamAccountBalanceConfig(account)
	if configValue == nil || !configValue.Configured {
		return nil, ErrUpstreamAccountBalanceConfigInvalid.WithMetadata(map[string]string{"reason": "not_configured"})
	}
	ciphertext, _ := account.Extra[UpstreamAccountBalancePasswordExtraKey].(string)
	if ciphertext == "" || s.encryptor == nil {
		return nil, ErrUpstreamAccountBalancePasswordRequired
	}
	password, err := s.encryptor.Decrypt(ciphertext)
	if err != nil || password == "" {
		return nil, ErrUpstreamAccountBalancePasswordRequired
	}

	now := time.Now().UTC()
	snapshot := s.queryNewAPI(ctx, account, configValue.Website, configValue.Email, password, now)
	if err := s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		UpstreamAccountBalanceSnapshotExtraKey: snapshot,
	}); err != nil {
		return nil, err
	}
	account.Extra = cloneMap(account.Extra)
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[UpstreamAccountBalanceSnapshotExtraKey] = snapshot
	return s.stateFromAccount(account), nil
}

func (s *UpstreamAccountBalanceService) queryNewAPI(
	ctx context.Context,
	account *Account,
	website, email, password string,
	now time.Time,
) *UpstreamAccountBalanceSnapshot {
	snapshot := &UpstreamAccountBalanceSnapshot{
		Status:        UpstreamAccountBalanceStatusFailed,
		LastAttemptAt: now,
		Unit:          "provider_quota",
	}
	loginPayload, _ := json.Marshal(map[string]string{"username": email, "password": password})
	loginURL := strings.TrimRight(website, "/") + "/api/user/login"
	loginResp, loginBody, err := s.doJSON(ctx, account, http.MethodPost, loginURL, loginPayload, "")
	if err != nil {
		snapshot.LastError = "login_request_failed"
		return snapshot
	}
	snapshot.HTTPStatus = loginResp
	if loginResp == http.StatusNotFound || loginResp == http.StatusMethodNotAllowed {
		snapshot.Status = UpstreamAccountBalanceStatusUnsupported
		snapshot.LastError = "login_endpoint_unsupported"
		return snapshot
	}
	if loginResp < 200 || loginResp >= 300 {
		snapshot.LastError = "login_rejected"
		return snapshot
	}
	token := extractString(loginBody, "data.token", "token", "data.access_token", "access_token")
	if token == "" {
		snapshot.LastError = "login_response_invalid"
		return snapshot
	}

	selfURL := strings.TrimRight(website, "/") + "/api/user/self"
	selfStatus, selfBody, err := s.doJSON(ctx, account, http.MethodGet, selfURL, nil, token)
	if err != nil {
		snapshot.LastError = "balance_request_failed"
		return snapshot
	}
	snapshot.HTTPStatus = selfStatus
	if selfStatus == http.StatusNotFound || selfStatus == http.StatusMethodNotAllowed {
		snapshot.Status = UpstreamAccountBalanceStatusUnsupported
		snapshot.LastError = "balance_endpoint_unsupported"
		return snapshot
	}
	if selfStatus < 200 || selfStatus >= 300 {
		snapshot.LastError = "balance_request_rejected"
		return snapshot
	}
	parsed := parseUpstreamAccountBalance(selfBody)
	if parsed == nil {
		snapshot.LastError = "balance_fields_not_found"
		return snapshot
	}
	*snapshot = *parsed
	snapshot.Status = UpstreamAccountBalanceStatusOK
	snapshot.LastAttemptAt = now
	snapshot.RetrievedAt = &now
	snapshot.HTTPStatus = selfStatus
	return snapshot
}

func (s *UpstreamAccountBalanceService) doJSON(
	ctx context.Context,
	account *Account,
	method, target string,
	body []byte,
	token string,
) (int, map[string]any, error) {
	requestCtx, cancel := context.WithTimeout(ctx, upstreamAccountBalanceRequestTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(requestCtx, method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	proxyURL := ""
	if account != nil && account.ProxyID != nil && account.Proxy != nil && account.Proxy.ID == *account.ProxyID {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	if err != nil || resp == nil || resp.Body == nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, upstreamAccountBalanceMaxBodyBytes+1))
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	if len(raw) > upstreamAccountBalanceMaxBodyBytes {
		return resp.StatusCode, nil, errors.New("response too large")
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, payload, nil
}

func parseUpstreamAccountBalance(payload map[string]any) *UpstreamAccountBalanceSnapshot {
	root := payload
	if nested, ok := payload["data"].(map[string]any); ok {
		root = nested
	}
	balance := findNumber(root, "balance", "credit", "credits", "remaining_balance", "remaining_credit")
	rawQuota := findNumber(root, "quota", "total_quota", "quota_limit", "limit", "total")
	usedQuota := findNumber(root, "used_quota", "quota_used", "used", "used_balance")
	remainingQuota := findNumber(root, "remaining_quota", "quota_remaining")
	if remainingQuota == nil && rawQuota != nil && usedQuota != nil {
		value := *rawQuota - *usedQuota
		if value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			remainingQuota = &value
		}
	}
	if balance == nil && remainingQuota == nil && rawQuota == nil {
		return nil
	}
	return &UpstreamAccountBalanceSnapshot{
		Balance:        balance,
		RawQuota:       rawQuota,
		UsedQuota:      usedQuota,
		RemainingQuota: remainingQuota,
		Unit:           "provider_quota",
	}
}

func findNumber(root map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		if raw, ok := root[key]; ok {
			if value, ok := numberValue(raw); ok {
				return &value
			}
		}
	}
	return nil
}

func numberValue(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, !math.IsNaN(value) && !math.IsInf(value, 0)
	case float32:
		return float64(value), !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func extractString(payload map[string]any, paths ...string) string {
	for _, path := range paths {
		var current any = payload
		for _, segment := range strings.Split(path, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[segment]
		}
		if value, ok := current.(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *UpstreamAccountBalanceService) stateFromAccount(account *Account) *UpstreamAccountBalanceState {
	if account == nil {
		return nil
	}
	configValue := upstreamAccountBalanceConfig(account)
	state := &UpstreamAccountBalanceState{AccountID: account.ID}
	if configValue != nil {
		state.Configured = configValue.Configured
		state.Config = configValue
	}
	_, state.PasswordConfigured = upstreamAccountBalancePassword(account)
	state.Snapshot = decodeUpstreamAccountBalanceSnapshot(account.Extra)
	return state
}

func upstreamAccountBalanceConfig(account *Account) *UpstreamAccountBalanceConfig {
	if account == nil || account.Extra == nil {
		return nil
	}
	raw, ok := account.Extra[UpstreamAccountBalanceConfigExtraKey].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	provider, _ := raw["provider"].(string)
	website, _ := raw["website"].(string)
	email, _ := raw["email"].(string)
	if provider == "" {
		provider = UpstreamAccountBalanceProviderNewAPI
	}
	return &UpstreamAccountBalanceConfig{
		Provider:   provider,
		Website:    website,
		Email:      email,
		Configured: strings.TrimSpace(website) != "" && strings.TrimSpace(email) != "",
	}
}

func upstreamAccountBalancePassword(account *Account) (string, bool) {
	if account == nil || account.Extra == nil {
		return "", false
	}
	ciphertext, _ := account.Extra[UpstreamAccountBalancePasswordExtraKey].(string)
	return ciphertext, strings.TrimSpace(ciphertext) != ""
}

func decodeUpstreamAccountBalanceSnapshot(extra map[string]any) *UpstreamAccountBalanceSnapshot {
	if extra == nil {
		return nil
	}
	raw, ok := extra[UpstreamAccountBalanceSnapshotExtraKey]
	if !ok || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snapshot UpstreamAccountBalanceSnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return nil
	}
	return &snapshot
}

func validateUpstreamAccountBalanceWebsite(cfg *config.Config, raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", errors.New("website is required")
	}
	if cfg != nil && cfg.Security.URLAllowlist.Enabled {
		return urlvalidator.ValidateHTTPSURL(trimmed, urlvalidator.ValidationOptions{
			AllowedHosts:     cfg.Security.URLAllowlist.UpstreamHosts,
			RequireAllowlist: true,
			AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
		})
	}
	allowInsecureHTTP := cfg != nil && cfg.Security.URLAllowlist.AllowInsecureHTTP
	return urlvalidator.ValidateURLFormat(trimmed, allowInsecureHTTP)
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
