package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client wraps HTTP calls to the management API.
type Client struct {
	baseURL   string
	secretKey string
	http      *http.Client
}

// NewClient creates a new management API client.
func NewClient(port int, secretKey string) *Client {
	return &Client{
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		secretKey: strings.TrimSpace(secretKey),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetSecretKey updates management API bearer token used by this client.
func (c *Client) SetSecretKey(secretKey string) {
	c.secretKey = strings.TrimSpace(secretKey)
}

func (c *Client) doRequest(method, path string, body io.Reader) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, err
	}
	if c.secretKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.secretKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) get(path string) ([]byte, error) {
	data, code, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *Client) put(path string, body io.Reader) ([]byte, error) {
	data, code, err := c.doRequest("PUT", path, body)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *Client) patch(path string, body io.Reader) ([]byte, error) {
	data, code, err := c.doRequest("PATCH", path, body)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// getJSON fetches a path and unmarshals JSON into a generic map.
func (c *Client) getJSON(path string) (map[string]any, error) {
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// postJSON sends a JSON body via POST and checks for errors.
func (c *Client) postJSON(path string, body any) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, code, err := c.doRequest("POST", path, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

// GetConfig fetches the parsed config.
func (c *Client) GetConfig() (map[string]any, error) {
	return c.getJSON("/v0/management/config")
}

// GetConfigYAML fetches the raw config.yaml content.
func (c *Client) GetConfigYAML() (string, error) {
	data, err := c.get("/v0/management/config.yaml")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// PutConfigYAML uploads new config.yaml content.
func (c *Client) PutConfigYAML(yamlContent string) error {
	_, err := c.put("/v0/management/config.yaml", strings.NewReader(yamlContent))
	return err
}

// GetAuthFiles lists auth credential files.
// API returns {"files": [...]}.
func (c *Client) GetAuthFiles() ([]map[string]any, error) {
	var files []map[string]any
	if err := c.getWrappedList("/v0/management/auth-files", "files", &files); err != nil {
		return nil, err
	}
	return files, nil
}

// DeleteAuthFile deletes a single auth file by name.
func (c *Client) DeleteAuthFile(name string) error {
	query := url.Values{}
	query.Set("name", name)
	path := "/v0/management/auth-files?" + query.Encode()
	_, code, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("delete failed (HTTP %d)", code)
	}
	return nil
}

// ToggleAuthFile enables or disables an auth file.
func (c *Client) ToggleAuthFile(name string, disabled bool) error {
	body, _ := json.Marshal(map[string]any{"name": name, "disabled": disabled})
	_, err := c.patch("/v0/management/auth-files/status", strings.NewReader(string(body)))
	return err
}

// PatchAuthFileFields updates editable fields on an auth file.
func (c *Client) PatchAuthFileFields(name string, fields map[string]any) error {
	fields["name"] = name
	body, _ := json.Marshal(fields)
	_, err := c.patch("/v0/management/auth-files/fields", strings.NewReader(string(body)))
	return err
}

// LogsResult is the decoded view of the logs endpoint response.
type LogsResult struct {
	Lines           []string
	LatestTimestamp int64
	NextCursor      string
	CursorReset     bool
}

// GetLogs fetches log lines from the server. Pass the next-cursor returned by
// a previous call to read only the lines appended since then; an empty cursor
// starts a fresh tail read of up to limit lines.
func (c *Client) GetLogs(cursor string, limit int) (LogsResult, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	path := "/v0/management/logs"
	if encodedQuery := query.Encode(); encodedQuery != "" {
		path += "?" + encodedQuery
	}

	data, err := c.get(path)
	if err != nil {
		return LogsResult{}, err
	}
	var resp struct {
		Lines           []string `json:"lines"`
		LatestTimestamp int64    `json:"latest-timestamp"`
		NextCursor      string   `json:"next-cursor"`
		CursorReset     bool     `json:"cursor-reset"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return LogsResult{}, err
	}
	if resp.Lines == nil {
		resp.Lines = []string{}
	}
	return LogsResult{
		Lines:           resp.Lines,
		LatestTimestamp: resp.LatestTimestamp,
		NextCursor:      resp.NextCursor,
		CursorReset:     resp.CursorReset,
	}, nil
}

// GetAPIKeys fetches the list of API keys.
// API returns {"api-keys": [...]}.
func (c *Client) GetAPIKeys() ([]string, error) {
	var keys []string
	if err := c.getWrappedList("/v0/management/api-keys", "api-keys", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// AddAPIKey adds a new API key by sending old=nil, new=key which appends.
func (c *Client) AddAPIKey(key string) error {
	body := map[string]any{"old": nil, "new": key}
	jsonBody, _ := json.Marshal(body)
	_, err := c.patch("/v0/management/api-keys", strings.NewReader(string(jsonBody)))
	return err
}

// EditAPIKey replaces an API key at the given index.
func (c *Client) EditAPIKey(index int, newValue string) error {
	body := map[string]any{"index": index, "value": newValue}
	jsonBody, _ := json.Marshal(body)
	_, err := c.patch("/v0/management/api-keys", strings.NewReader(string(jsonBody)))
	return err
}

// DeleteAPIKey deletes an API key by index.
func (c *Client) DeleteAPIKey(index int) error {
	_, code, err := c.doRequest("DELETE", fmt.Sprintf("/v0/management/api-keys?index=%d", index), nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("delete failed (HTTP %d)", code)
	}
	return nil
}

// GetGeminiKeys fetches Gemini API keys.
// API returns {"gemini-api-key": [...]}.
func (c *Client) GetGeminiKeys() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/gemini-api-key", "gemini-api-key", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetInteractionsKeys fetches native Interactions API keys.
// API returns {"interactions-api-key": [...]}.
func (c *Client) GetInteractionsKeys() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/interactions-api-key", "interactions-api-key", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetClaudeKeys fetches Claude API keys.
func (c *Client) GetClaudeKeys() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/claude-api-key", "claude-api-key", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetCodexKeys fetches Codex API keys.
func (c *Client) GetCodexKeys() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/codex-api-key", "codex-api-key", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetXAIKeys fetches xAI API keys.
func (c *Client) GetXAIKeys() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/xai-api-key", "xai-api-key", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetVertexKeys fetches Vertex API keys.
func (c *Client) GetVertexKeys() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/vertex-api-key", "vertex-api-key", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// GetOpenAICompat fetches OpenAI compatibility entries.
func (c *Client) GetOpenAICompat() ([]map[string]any, error) {
	var keys []map[string]any
	if err := c.getWrappedList("/v0/management/openai-compatibility", "openai-compatibility", &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// getWrappedList fetches a JSON object and decodes the array under key
// directly into target (a non-nil pointer to a slice), avoiding a
// map[string]any marshal/unmarshal round-trip. A missing or null array
// leaves target untouched.
func (c *Client) getWrappedList(path, key string, target any) error {
	data, err := c.get(path)
	if err != nil {
		return err
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	raw, ok := wrapper[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	return json.Unmarshal(raw, target)
}

// GetDebug fetches the current debug setting.
func (c *Client) GetDebug() (bool, error) {
	wrapper, err := c.getJSON("/v0/management/debug")
	if err != nil {
		return false, err
	}
	if v, ok := wrapper["debug"]; ok {
		if b, ok := v.(bool); ok {
			return b, nil
		}
	}
	return false, nil
}

// GetAuthStatus polls the OAuth session status.
// Returns status ("wait", "ok", "error") and optional error message.
func (c *Client) GetAuthStatus(state string) (string, string, error) {
	query := url.Values{}
	query.Set("state", state)
	path := "/v0/management/get-auth-status?" + query.Encode()
	wrapper, err := c.getJSON(path)
	if err != nil {
		return "", "", err
	}
	status := getString(wrapper, "status")
	errMsg := getString(wrapper, "error")
	return status, errMsg, nil
}

// CancelAuthSession cancels a pending OAuth session on the management server.
func (c *Client) CancelAuthSession(state string) error {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil
	}
	query := url.Values{}
	query.Set("state", state)
	path := "/v0/management/oauth-session?" + query.Encode()
	_, code, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

// ----- Config field update methods -----

// PutBoolField updates a boolean config field.
func (c *Client) PutBoolField(path string, value bool) error {
	body, _ := json.Marshal(map[string]any{"value": value})
	_, err := c.put("/v0/management/"+path, strings.NewReader(string(body)))
	return err
}

// PutIntField updates an integer config field.
func (c *Client) PutIntField(path string, value int) error {
	body, _ := json.Marshal(map[string]any{"value": value})
	_, err := c.put("/v0/management/"+path, strings.NewReader(string(body)))
	return err
}

// PutStringField updates a string config field.
func (c *Client) PutStringField(path string, value string) error {
	body, _ := json.Marshal(map[string]any{"value": value})
	_, err := c.put("/v0/management/"+path, strings.NewReader(string(body)))
	return err
}

// DeleteField sends a DELETE request for a config field.
func (c *Client) DeleteField(path string) error {
	_, _, err := c.doRequest("DELETE", "/v0/management/"+path, nil)
	return err
}
