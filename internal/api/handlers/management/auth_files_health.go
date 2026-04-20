package management

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func (h *Handler) findAuthByNameOrID(name string) *coreauth.Auth {
	if h == nil || h.authManager == nil {
		return nil
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if auth, ok := h.authManager.GetByID(name); ok {
		return auth
	}

	auths := h.authManager.List()
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		if strings.TrimSpace(auth.FileName) == name {
			return auth
		}
		if filepathName := strings.TrimSpace(authAttribute(auth, "path")); filepathName != "" {
			if filepathName == name {
				return auth
			}
		}
	}
	return nil
}

func accountHealthResponse(state *coreauth.AccountHealthState, now time.Time) gin.H {
	if state == nil {
		return gin.H{}
	}
	clone := state.Clone()
	if clone == nil {
		return gin.H{}
	}
	clone.Stale = clone.IsStale(now)
	response := gin.H{
		"degraded":             clone.Degraded,
		"degraded_reason":      clone.DegradedReason,
		"degraded_status":      clone.DegradedStatus,
		"degraded_message":     clone.DegradedMessage,
		"consecutive_failures": clone.ConsecutiveFailures,
		"failure_statuses":     clone.FailureStatuses,
		"degraded_at":          clone.DegradedAt,
		"manual_degraded":      clone.ManualDegraded,
		"stale":                clone.Stale,
	}
	if clone.CooldownUntil == nil {
		response["cooldown_until"] = nil
	} else {
		response["cooldown_until"] = *clone.CooldownUntil
	}
	return response
}

func syncAuthStateFromHealth(auth *coreauth.Auth, now time.Time) {
	if auth == nil {
		return
	}
	auth.Unavailable = false
	auth.NextRetryAfter = time.Time{}
	if auth.Disabled {
		auth.Status = coreauth.StatusDisabled
		auth.StatusMessage = "disabled via management API"
		return
	}

	auth.Status = coreauth.StatusActive
	if auth.StatusMessage == "disabled via management API" {
		auth.StatusMessage = ""
	}
	auth.ApplyPersistedAccountHealth(now)
}

func optionalBoolField(raw map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err == nil {
				return parsed, true
			}
		case float64:
			return typed != 0, true
		case int:
			return typed != 0, true
		case int64:
			return typed != 0, true
		}
	}
	return false, false
}

func optionalStringField(raw map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if value == nil {
			return "", true
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed), true
		case json.Number:
			return strings.TrimSpace(typed.String()), true
		default:
			payload, err := json.Marshal(typed)
			if err != nil {
				return "", false
			}
			return strings.TrimSpace(strings.Trim(string(payload), `"`)), true
		}
	}
	return "", false
}

func optionalInt64Field(raw map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case int:
			return int64(typed), true
		case int32:
			return int64(typed), true
		case int64:
			return typed, true
		case float64:
			return int64(typed), true
		case json.Number:
			parsed, err := typed.Int64()
			if err == nil {
				return parsed, true
			}
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func optionalNullableInt64Field(raw map[string]any, keys ...string) (int64, bool, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if value == nil {
			return 0, true, true
		}
		parsed, okParse := optionalInt64Field(map[string]any{key: value}, key)
		if okParse {
			return parsed, true, false
		}
	}
	return 0, false, false
}

func (h *Handler) GetAuthFileHealth(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	now := time.Now()
	response := make(map[string]gin.H)
	for _, auth := range h.authManager.List() {
		if auth == nil {
			continue
		}
		state, ok := auth.AccountHealth()
		if !ok || state == nil {
			continue
		}
		name := strings.TrimSpace(auth.FileName)
		if name == "" {
			name = strings.TrimSpace(auth.ID)
		}
		if name == "" {
			continue
		}
		response[name] = accountHealthResponse(state, now)
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handler) PutAuthFileHealth(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	source := payload
	for _, key := range []string{"health", "items", "data"} {
		if nested, ok := payload[key].(map[string]any); ok {
			source = nested
			break
		}
	}
	if len(source) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "health updates are required"})
		return
	}

	type updateItem struct {
		auth  *coreauth.Auth
		clear bool
		state *coreauth.AccountHealthState
	}

	updates := make([]updateItem, 0, len(source))
	for rawName, rawState := range source {
		name := strings.TrimSpace(rawName)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "health update key is empty"})
			return
		}
		auth := h.findAuthByNameOrID(name)
		if auth == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("auth file not found: %s", name)})
			return
		}
		if rawState == nil {
			updates = append(updates, updateItem{auth: auth, clear: true})
			continue
		}
		state, ok := coreauth.ParseAccountHealthState(rawState)
		if !ok || state == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid health state for %s", name)})
			return
		}
		updates = append(updates, updateItem{auth: auth, state: state})
	}

	ctx := c.Request.Context()
	now := time.Now()
	response := make(map[string]gin.H, len(updates))
	for _, update := range updates {
		auth := update.auth
		if update.clear || update.state == nil || !update.state.Degraded {
			auth.ClearAccountHealth()
		} else {
			if update.state.DegradedAt <= 0 {
				update.state.DegradedAt = now.UnixMilli()
			}
			auth.SetAccountHealth(update.state)
		}
		syncAuthStateFromHealth(auth, now)
		auth.UpdatedAt = now
		if _, err := h.authManager.Update(ctx, auth); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth health: %v", err)})
			return
		}

		name := strings.TrimSpace(auth.FileName)
		if name == "" {
			name = strings.TrimSpace(auth.ID)
		}
		if state, ok := auth.AccountHealth(); ok && state != nil {
			response[name] = accountHealthResponse(state, now)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "health": response})
}

func (h *Handler) RecoverAuthFileHealth(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	auth := h.findAuthByNameOrID(name)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}

	now := time.Now()
	auth.ClearAccountHealth()
	syncAuthStateFromHealth(auth, now)
	auth.UpdatedAt = now

	if _, err := h.authManager.Update(c.Request.Context(), auth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to recover auth health: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "name": auth.FileName})
}
