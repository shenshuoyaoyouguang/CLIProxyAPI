// Package kimi provides authentication and token management for Kimi (Moonshot AI) API.
// It handles the RFC 8628 OAuth2 Device Authorization Grant flow for secure authentication.
package kimi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	// kimiClientID is Kimi Code's OAuth client ID.
	kimiClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"
	// kimiOAuthHost is the OAuth server endpoint.
	kimiOAuthHost = "https://auth.kimi.com"
	// kimiDeviceCodeURL is the endpoint for requesting device codes.
	kimiDeviceCodeURL = kimiOAuthHost + "/api/oauth/device_authorization"
	// kimiTokenURL is the endpoint for exchanging device codes for tokens.
	kimiTokenURL = kimiOAuthHost + "/api/oauth/token"
	// KimiAPIBaseURL is the base URL for Kimi API requests.
	KimiAPIBaseURL = "https://api.kimi.com/coding"
	// defaultPollInterval is the default interval for polling token endpoint.
	defaultPollInterval = 5 * time.Second
	// maxPollInterval is the upper bound when server asks client to slow down.
	maxPollInterval = 30 * time.Second
	// slowDownStep is the interval increment applied after slow_down responses.
	slowDownStep = 2 * time.Second
	// maxPollDuration is the maximum time to wait for user authorization.
	maxPollDuration = 15 * time.Minute
	// refreshThresholdSeconds is when to refresh token before expiry (5 minutes).
	refreshThresholdSeconds = 300
)

var (
	errKimiAuthorizationPending = errors.New("kimi: authorization_pending")
	errKimiSlowDown             = errors.New("kimi: slow_down")
	errKimiExpiredToken         = errors.New("kimi: expired_token")
	errKimiAccessDenied         = errors.New("kimi: access_denied")
)

// KimiAuth handles Kimi authentication flow.
type KimiAuth struct {
	deviceClient *DeviceFlowClient
	cfg          *config.Config
}

// NewKimiAuth creates a new KimiAuth service instance.
func NewKimiAuth(cfg *config.Config) *KimiAuth {
	return &KimiAuth{
		deviceClient: NewDeviceFlowClient(cfg),
		cfg:          cfg,
	}
}

// StartDeviceFlow initiates the device flow authentication.
func (k *KimiAuth) StartDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	return k.deviceClient.RequestDeviceCode(ctx)
}

// WaitForAuthorization polls for user authorization and returns the auth bundle.
func (k *KimiAuth) WaitForAuthorization(ctx context.Context, deviceCode *DeviceCodeResponse) (*KimiAuthBundle, error) {
	tokenData, err := k.deviceClient.PollForToken(ctx, deviceCode)
	if err != nil {
		return nil, err
	}

	return &KimiAuthBundle{
		TokenData: tokenData,
		DeviceID:  k.deviceClient.deviceID,
	}, nil
}

// CreateTokenStorage creates a new KimiTokenStorage from auth bundle.
func (k *KimiAuth) CreateTokenStorage(bundle *KimiAuthBundle) *KimiTokenStorage {
	expired := ""
	if bundle.TokenData.ExpiresAt > 0 {
		expired = time.Unix(bundle.TokenData.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	return &KimiTokenStorage{
		AccessToken:  bundle.TokenData.AccessToken,
		RefreshToken: bundle.TokenData.RefreshToken,
		TokenType:    bundle.TokenData.TokenType,
		Scope:        bundle.TokenData.Scope,
		DeviceID:     strings.TrimSpace(bundle.DeviceID),
		Expired:      expired,
		Type:         "kimi",
	}
}

// DeviceFlowClient handles the OAuth2 device flow for Kimi.
type DeviceFlowClient struct {
	httpClient *http.Client
	cfg        *config.Config
	deviceID   string
}

// NewDeviceFlowClient creates a new device flow client.
func NewDeviceFlowClient(cfg *config.Config) *DeviceFlowClient {
	return NewDeviceFlowClientWithDeviceID(cfg, "")
}

// NewDeviceFlowClientWithDeviceID creates a new device flow client with the specified device ID.
func NewDeviceFlowClientWithDeviceID(cfg *config.Config, deviceID string) *DeviceFlowClient {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	resolvedDeviceID := strings.TrimSpace(deviceID)
	if resolvedDeviceID == "" {
		resolvedDeviceID = getOrCreateDeviceID()
	}
	return &DeviceFlowClient{
		httpClient: client,
		cfg:        cfg,
		deviceID:   resolvedDeviceID,
	}
}

// getOrCreateDeviceID returns an in-memory device ID for the current authentication flow.
func getOrCreateDeviceID() string {
	return uuid.New().String()
}

// getDeviceModel returns a device model string.
func getDeviceModel() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	switch osName {
	case "darwin":
		return fmt.Sprintf("macOS %s", arch)
	case "windows":
		return fmt.Sprintf("Windows %s", arch)
	case "linux":
		return fmt.Sprintf("Linux %s", arch)
	default:
		return fmt.Sprintf("%s %s", osName, arch)
	}
}

// getHostname returns the machine hostname.
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// commonHeaders returns headers required for Kimi API requests.
func (c *DeviceFlowClient) commonHeaders() map[string]string {
	return map[string]string{
		"X-Msh-Platform":     "cli-proxy-api",
		"X-Msh-Version":      "1.0.0",
		"X-Msh-Device-Name":  getHostname(),
		"X-Msh-Device-Model": getDeviceModel(),
		"X-Msh-Device-Id":    c.deviceID,
	}
}

// RequestDeviceCode initiates the device flow by requesting a device code from Kimi.
func (c *DeviceFlowClient) RequestDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	data := url.Values{}
	data.Set("client_id", kimiClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kimiDeviceCodeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.commonHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: device code request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("kimi device code: close body error: %v", errClose)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi: device code request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var deviceCode DeviceCodeResponse
	if err = json.Unmarshal(bodyBytes, &deviceCode); err != nil {
		return nil, fmt.Errorf("kimi: failed to parse device code response: %w", err)
	}

	return &deviceCode, nil
}

// PollForToken polls the token endpoint until the user authorizes or the device code expires.
func (c *DeviceFlowClient) PollForToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*KimiTokenData, error) {
	if deviceCode == nil {
		return nil, fmt.Errorf("kimi: device code is nil")
	}
	if strings.TrimSpace(deviceCode.DeviceCode) == "" {
		return nil, fmt.Errorf("kimi: device code is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < defaultPollInterval {
		interval = defaultPollInterval
	}
	currentInterval := interval

	deadline := time.Now().Add(maxPollDuration)
	if deviceCode.ExpiresIn > 0 {
		codeDeadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)
		if codeDeadline.Before(deadline) {
			deadline = codeDeadline
		}
	}

	for {
		if err := waitForNextKimiPoll(ctx, currentInterval); err != nil {
			return nil, fmt.Errorf("kimi: context cancelled: %w", err)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("kimi: device code expired: %w", errKimiExpiredToken)
		}

		token, pollErr := c.exchangeDeviceCode(ctx, deviceCode.DeviceCode)
		if pollErr == nil {
			return token, nil
		}

		switch {
		case errors.Is(pollErr, errKimiAuthorizationPending):
			// Keep current interval.
		case errors.Is(pollErr, errKimiSlowDown):
			currentInterval += slowDownStep
			if currentInterval > maxPollInterval {
				currentInterval = maxPollInterval
			}
		case errors.Is(pollErr, errKimiExpiredToken):
			return nil, fmt.Errorf("kimi: device code expired: %w", pollErr)
		case errors.Is(pollErr, errKimiAccessDenied):
			return nil, fmt.Errorf("kimi: access denied by user: %w", pollErr)
		case errors.Is(pollErr, context.Canceled), errors.Is(pollErr, context.DeadlineExceeded):
			return nil, fmt.Errorf("kimi: context cancelled: %w", pollErr)
		default:
			return nil, pollErr
		}
	}
}

// exchangeDeviceCode attempts to exchange the device code for an access token.
func (c *DeviceFlowClient) exchangeDeviceCode(ctx context.Context, deviceCode string) (*KimiTokenData, error) {
	data := url.Values{}
	data.Set("client_id", kimiClientID)
	data.Set("device_code", deviceCode)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kimiTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.commonHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: token request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("kimi token exchange: close body error: %v", errClose)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to read token response: %w", err)
	}

	// Parse response - Kimi returns 200 for both success and pending states
	var oauthResp struct {
		Error            string  `json:"error"`
		ErrorDescription string  `json:"error_description"`
		AccessToken      string  `json:"access_token"`
		RefreshToken     string  `json:"refresh_token"`
		TokenType        string  `json:"token_type"`
		ExpiresIn        float64 `json:"expires_in"`
		Scope            string  `json:"scope"`
	}

	if err = json.Unmarshal(bodyBytes, &oauthResp); err != nil {
		return nil, fmt.Errorf("kimi: failed to parse token response: %w", err)
	}

	if oauthResp.Error != "" {
		switch oauthResp.Error {
		case "authorization_pending":
			if strings.TrimSpace(oauthResp.ErrorDescription) == "" {
				return nil, errKimiAuthorizationPending
			}
			return nil, fmt.Errorf("%w: %s", errKimiAuthorizationPending, oauthResp.ErrorDescription)
		case "slow_down":
			if strings.TrimSpace(oauthResp.ErrorDescription) == "" {
				return nil, errKimiSlowDown
			}
			return nil, fmt.Errorf("%w: %s", errKimiSlowDown, oauthResp.ErrorDescription)
		case "expired_token":
			if strings.TrimSpace(oauthResp.ErrorDescription) == "" {
				return nil, errKimiExpiredToken
			}
			return nil, fmt.Errorf("%w: %s", errKimiExpiredToken, oauthResp.ErrorDescription)
		case "access_denied":
			if strings.TrimSpace(oauthResp.ErrorDescription) == "" {
				return nil, errKimiAccessDenied
			}
			return nil, fmt.Errorf("%w: %s", errKimiAccessDenied, oauthResp.ErrorDescription)
		default:
			return nil, fmt.Errorf("kimi: OAuth error: %s - %s", oauthResp.Error, oauthResp.ErrorDescription)
		}
	}

	if oauthResp.AccessToken == "" {
		return nil, fmt.Errorf("kimi: empty access token in response")
	}

	var expiresAt int64
	if oauthResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + int64(oauthResp.ExpiresIn)
	}

	return &KimiTokenData{
		AccessToken:  oauthResp.AccessToken,
		RefreshToken: oauthResp.RefreshToken,
		TokenType:    oauthResp.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        oauthResp.Scope,
	}, nil
}

func waitForNextKimiPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *DeviceFlowClient) RefreshToken(ctx context.Context, refreshToken string) (*KimiTokenData, error) {
	data := url.Values{}
	data.Set("client_id", kimiClientID)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kimiTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.commonHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: refresh request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("kimi refresh token: close body error: %v", errClose)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to read refresh response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("kimi: refresh token rejected (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi: refresh failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		TokenType    string  `json:"token_type"`
		ExpiresIn    float64 `json:"expires_in"`
		Scope        string  `json:"scope"`
	}

	if err = json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("kimi: failed to parse refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("kimi: empty access token in refresh response")
	}

	var expiresAt int64
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + int64(tokenResp.ExpiresIn)
	}

	return &KimiTokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        tokenResp.Scope,
	}, nil
}
