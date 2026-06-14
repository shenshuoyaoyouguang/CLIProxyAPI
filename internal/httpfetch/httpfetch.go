package httpfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Doer abstracts the HTTP client used to execute requests.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// GetBytes performs a GET request with the supplied headers, requires a
// success status, and returns the response body. When maxSize is positive
// the body is rejected once it exceeds maxSize bytes.
func GetBytes(ctx context.Context, client Doer, requestURL string, headers map[string]string, maxSize int64) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if errRequest != nil {
		return nil, fmt.Errorf("create request: %w", errRequest)
	}
	req.Header.Set("User-Agent", "CLIProxyAPI/1.0")
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}

	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, fmt.Errorf("request failed: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("failed to close response body")
		}
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, errRead := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if errRead != nil {
			log.WithError(errRead).Debug("failed to read error response body")
		}
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	reader := io.Reader(resp.Body)
	if maxSize > 0 {
		reader = io.LimitReader(resp.Body, maxSize+1)
	}
	data, errRead := io.ReadAll(reader)
	if errRead != nil {
		return nil, fmt.Errorf("read response: %w", errRead)
	}
	if maxSize > 0 && int64(len(data)) > maxSize {
		return nil, fmt.Errorf("response exceeds maximum allowed size of %d bytes", maxSize)
	}
	return data, nil
}
