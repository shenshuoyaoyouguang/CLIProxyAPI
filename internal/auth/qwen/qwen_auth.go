package qwen

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	// QwenOAuthDeviceCodeEndpoint is the URL for initiating the OAuth 2.0 device authorization flow.
	QwenOAuthDeviceCodeEndpoint = "https://chat.qwen.ai/api/v1/oauth2/device/code"
	// QwenOAuthTokenEndpoint is the URL for exchanging device codes or refresh tokens for access tokens.
	QwenOAuthTokenEndpoint = "https://chat.qwen.ai/api/v1/oauth2/token"
	// QwenOAuthClientID is the client identifier for the Qwen OAuth 2.0 application.
	QwenOAuthClientID = "f0304373b74a44d2b584a3fb70ca9e56"
	// QwenOAuthScope defines the permissions requested by the application.
	QwenOAuthScope = "openid profile email model.completion"
	// QwenOAuthGrantType specifies the grant type for the device code flow.
	QwenOAuthGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	qwenDefaultPollInterval = 5 * time.Second
	qwenMaxPollInterval     = 30 * time.Second
	qwenSlowDownStep        = 2 * time.Second
	qwenMaxRetryBackoff     = 10 * time.Second
	qwenErrorBodyPreviewMax = 512
)

var (
	errQwenAuthorizationPending = errors.New("qwen: authorization_pending")
	errQwenSlowDown             = errors.New("qwen: slow_down")
	errQwenExpiredToken         = errors.New("qwen: expired_token")
	errQwenAccessDenied         = errors.New("qwen: access_denied")
)

// QwenTokenData represents the OAuth credentials, including access and refresh tokens.
type QwenTokenData struct {
	AccessToken string `json:"access_token"`
	// RefreshToken is used to obtain a new access token when the current one expires.
	RefreshToken string `json:"refresh_token,omitempty"`
	// TokenType indicates the type of token, typically "Bearer".
	TokenType string `json:"token_type"`
	// ResourceURL specifies the base URL of the resource server.
	ResourceURL string `json:"resource_url,omitempty"`
	// Expire indicates the expiration date and time of the access token.
	Expire string `json:"expiry_date,omitempty"`
}

// DeviceFlow represents the response from the device authorization endpoint.
type DeviceFlow struct {
	// DeviceCode is the code that the client uses to poll for an access token.
	DeviceCode string `json:"device_code"`
	// UserCode is the code that the user enters at the verification URI.
	UserCode string `json:"user_code"`
	// VerificationURI is the URL where the user can enter the user code to authorize the device.
	VerificationURI string `json:"verification_uri"`
	// VerificationURIComplete is a URI that includes the user_code, which can be used to automatically
	// fill in the code on the verification page.
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresIn is the time in seconds until the device_code and user_code expire.
	ExpiresIn int `json:"expires_in"`
	// Interval is the minimum time in seconds that the client should wait between polling requests.
	Interval int `json:"interval"`
	// CodeVerifier is the cryptographically random string used in the PKCE flow.
	CodeVerifier string `json:"code_verifier"`
}

// QwenTokenResponse represents the successful token response from the token endpoint.
type QwenTokenResponse struct {
	// AccessToken is the token used to access protected resources.
	AccessToken string `json:"access_token"`
	// RefreshToken is used to obtain a new access token.
	RefreshToken string `json:"refresh_token,omitempty"`
	// TokenType indicates the type of token, typically "Bearer".
	TokenType string `json:"token_type"`
	// ResourceURL specifies the base URL of the resource server.
	ResourceURL string `json:"resource_url,omitempty"`
	// ExpiresIn is the time in seconds until the access token expires.
	ExpiresIn int `json:"expires_in"`
}

// QwenAuth manages authentication and token handling for the Qwen API.
type QwenAuth struct {
	httpClient *http.Client
}

// NewQwenAuth creates a new QwenAuth instance with a proxy-configured HTTP client.
func NewQwenAuth(cfg *config.Config) *QwenAuth {
	return &QwenAuth{
		httpClient: util.SetProxy(&cfg.SDKConfig, &http.Client{}),
	}
}

// generateCodeVerifier generates a cryptographically random string for the PKCE code verifier.
func (qa *QwenAuth) generateCodeVerifier() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// generateCodeChallenge creates a SHA-256 hash of the code verifier, used as the PKCE code challenge.
func (qa *QwenAuth) generateCodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// generatePKCEPair creates a new code verifier and its corresponding code challenge for PKCE.
func (qa *QwenAuth) generatePKCEPair() (string, string, error) {
	codeVerifier, err := qa.generateCodeVerifier()
	if err != nil {
		return "", "", err
	}
	codeChallenge := qa.generateCodeChallenge(codeVerifier)
	return codeVerifier, codeChallenge, nil
}

// RefreshTokens exchanges a refresh token for a new access token.
func (qa *QwenAuth) RefreshTokens(ctx context.Context, refreshToken string) (*QwenTokenData, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", QwenOAuthClientID)

	req, err := http.NewRequestWithContext(ctx, "POST", QwenOAuthTokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := qa.httpClient.Do(req)

	// resp, err := qa.httpClient.PostForm(QwenOAuthTokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, formatQwenOAuthError(body))
	}

	var tokenData QwenTokenResponse
	if err = json.Unmarshal(body, &tokenData); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &QwenTokenData{
		AccessToken:  tokenData.AccessToken,
		TokenType:    tokenData.TokenType,
		RefreshToken: tokenData.RefreshToken,
		ResourceURL:  tokenData.ResourceURL,
		Expire:       time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second).Format(time.RFC3339),
	}, nil
}

// InitiateDeviceFlow starts the OAuth 2.0 device authorization flow and returns the device flow details.
func (qa *QwenAuth) InitiateDeviceFlow(ctx context.Context) (*DeviceFlow, error) {
	// Generate PKCE code verifier and challenge
	codeVerifier, codeChallenge, err := qa.generatePKCEPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE pair: %w", err)
	}

	data := url.Values{}
	data.Set("client_id", QwenOAuthClientID)
	data.Set("scope", QwenOAuthScope)
	data.Set("code_challenge", codeChallenge)
	data.Set("code_challenge_method", "S256")

	req, err := http.NewRequestWithContext(ctx, "POST", QwenOAuthDeviceCodeEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := qa.httpClient.Do(req)

	// resp, err := qa.httpClient.PostForm(QwenOAuthDeviceCodeEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("device authorization request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization failed (status %d): %s", resp.StatusCode, formatQwenOAuthError(body))
	}

	var result DeviceFlow
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse device flow response: %w", err)
	}

	// Check if the response indicates success
	if result.DeviceCode == "" {
		return nil, fmt.Errorf("device authorization failed: device_code not found in response")
	}

	// Add the code_verifier to the result so it can be used later for polling
	result.CodeVerifier = codeVerifier

	return &result, nil
}

// PollForToken polls the token endpoint with the device code to obtain an access token.
// intervalSeconds and expiresInSeconds should come from the device code response.
func (qa *QwenAuth) PollForToken(ctx context.Context, deviceCode, codeVerifier string, intervalSeconds, expiresInSeconds int) (*QwenTokenData, error) {
	if strings.TrimSpace(deviceCode) == "" {
		return nil, fmt.Errorf("qwen: device_code is required")
	}
	if strings.TrimSpace(codeVerifier) == "" {
		return nil, fmt.Errorf("qwen: code_verifier is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pollInterval := qwenDefaultPollInterval
	if intervalSeconds > 0 {
		pollInterval = time.Duration(intervalSeconds) * time.Second
	}
	if pollInterval < time.Second {
		pollInterval = time.Second
	}
	currentInterval := pollInterval

	pollCtx := ctx
	cancel := func() {}
	if expiresInSeconds > 0 {
		pollWindow := time.Duration(expiresInSeconds) * time.Second
		if deadline, hasDeadline := ctx.Deadline(); !hasDeadline || time.Until(deadline) > pollWindow {
			pollCtx, cancel = context.WithTimeout(ctx, pollWindow)
		}
	}
	defer cancel()

	for {
		tokenData, err := qa.pollTokenOnce(pollCtx, deviceCode, codeVerifier)
		if err == nil {
			return tokenData, nil
		}

		switch {
		case errors.Is(err, errQwenAuthorizationPending):
			// keep polling with current interval
		case errors.Is(err, errQwenSlowDown):
			currentInterval += qwenSlowDownStep
			if currentInterval > qwenMaxPollInterval {
				currentInterval = qwenMaxPollInterval
			}
		case errors.Is(err, errQwenExpiredToken):
			return nil, fmt.Errorf("qwen: device code expired, restart authentication: %w", err)
		case errors.Is(err, errQwenAccessDenied):
			return nil, fmt.Errorf("qwen: authorization denied by user: %w", err)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			if errors.Is(err, context.DeadlineExceeded) {
				log.WithError(err).Warn("qwen: device flow polling exceeded context deadline")
			}
			return nil, qwenContextError(err)
		default:
			return nil, err
		}

		if err = waitForNextQwenPoll(pollCtx, currentInterval); err != nil {
			return nil, qwenContextError(err)
		}
	}
}

func (qa *QwenAuth) pollTokenOnce(ctx context.Context, deviceCode, codeVerifier string) (*QwenTokenData, error) {
	data := url.Values{}
	data.Set("grant_type", QwenOAuthGrantType)
	data.Set("client_id", QwenOAuthClientID)
	data.Set("device_code", deviceCode)
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, QwenOAuthTokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("qwen: failed to create poll request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := qa.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("qwen: poll request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("qwen: failed to read poll response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorData map[string]interface{}
		if err = json.Unmarshal(body, &errorData); err == nil {
			errorType, _ := errorData["error"].(string)
			errorDesc, _ := errorData["error_description"].(string)
			return nil, mapQwenPollingError(errorType, errorDesc)
		}
		return nil, fmt.Errorf("qwen: device token poll failed (status %d): %s", resp.StatusCode, trimQwenErrorPreview(body))
	}

	var response QwenTokenResponse
	if err = json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("qwen: failed to parse token response: %w", err)
	}

	if strings.TrimSpace(response.AccessToken) == "" {
		return nil, fmt.Errorf("qwen: empty access token in response")
	}

	return &QwenTokenData{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		TokenType:    response.TokenType,
		ResourceURL:  response.ResourceURL,
		Expire:       time.Now().Add(time.Duration(response.ExpiresIn) * time.Second).Format(time.RFC3339),
	}, nil
}

func mapQwenPollingError(errorType, errorDescription string) error {
	description := strings.TrimSpace(errorDescription)

	switch errorType {
	case "authorization_pending":
		if description == "" {
			return errQwenAuthorizationPending
		}
		return fmt.Errorf("%w: %s", errQwenAuthorizationPending, description)
	case "slow_down":
		if description == "" {
			return errQwenSlowDown
		}
		return fmt.Errorf("%w: %s", errQwenSlowDown, description)
	case "expired_token":
		if description == "" {
			return errQwenExpiredToken
		}
		return fmt.Errorf("%w: %s", errQwenExpiredToken, description)
	case "access_denied":
		if description == "" {
			return errQwenAccessDenied
		}
		return fmt.Errorf("%w: %s", errQwenAccessDenied, description)
	default:
		if strings.TrimSpace(errorType) == "" {
			errorType = "unknown_error"
		}
		return fmt.Errorf("qwen: device token poll failed: %s - %s", errorType, description)
	}
}

func waitForNextQwenPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func qwenContextError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("qwen: polling cancelled: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("qwen: polling timeout: %w", err)
	default:
		return fmt.Errorf("qwen: polling interrupted: %w", err)
	}
}

func formatQwenOAuthError(body []byte) string {
	var errorData struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &errorData); err == nil {
		errType := strings.TrimSpace(errorData.Error)
		errDesc := strings.TrimSpace(errorData.ErrorDescription)
		switch {
		case errType != "" && errDesc != "":
			return fmt.Sprintf("%s - %s", errType, errDesc)
		case errType != "":
			return errType
		case errDesc != "":
			return errDesc
		}
	}
	return trimQwenErrorPreview(body)
}

func trimQwenErrorPreview(body []byte) string {
	preview := strings.TrimSpace(string(body))
	if preview == "" {
		return "empty response body"
	}
	if len(preview) <= qwenErrorBodyPreviewMax {
		return preview
	}
	return fmt.Sprintf("%s...(truncated)", preview[:qwenErrorBodyPreviewMax])
}

func qwenRetryDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	delay := time.Duration(attempt) * time.Second
	if delay > qwenMaxRetryBackoff {
		return qwenMaxRetryBackoff
	}
	return delay
}

// RefreshTokensWithRetry attempts to refresh tokens with a specified number of retries upon failure.
func (o *QwenAuth) RefreshTokensWithRetry(ctx context.Context, refreshToken string, maxRetries int) (*QwenTokenData, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := qwenRetryDelay(attempt)
			log.Debugf("qwen: waiting %s before retry attempt %d", delay, attempt+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		tokenData, err := o.RefreshTokens(ctx, refreshToken)
		if err == nil {
			return tokenData, nil
		}

		lastErr = err
		log.Warnf("Token refresh attempt %d failed: %v", attempt+1, err)
	}

	return nil, fmt.Errorf("token refresh failed after %d attempts: %w", maxRetries, lastErr)
}

// CreateTokenStorage creates a QwenTokenStorage object from a QwenTokenData object.
func (o *QwenAuth) CreateTokenStorage(tokenData *QwenTokenData) *QwenTokenStorage {
	storage := &QwenTokenStorage{
		AccessToken:  tokenData.AccessToken,
		RefreshToken: tokenData.RefreshToken,
		LastRefresh:  time.Now().Format(time.RFC3339),
		ResourceURL:  tokenData.ResourceURL,
		Expire:       tokenData.Expire,
	}

	return storage
}

// UpdateTokenStorage updates an existing token storage with new token data
func (o *QwenAuth) UpdateTokenStorage(storage *QwenTokenStorage, tokenData *QwenTokenData) {
	storage.AccessToken = tokenData.AccessToken
	storage.RefreshToken = tokenData.RefreshToken
	storage.LastRefresh = time.Now().Format(time.RFC3339)
	storage.ResourceURL = tokenData.ResourceURL
	storage.Expire = tokenData.Expire
}
