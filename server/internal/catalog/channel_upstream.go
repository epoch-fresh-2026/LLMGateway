package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"LLMGateway/server/internal/httpcommon"
)

func (a *Server) remoteModels(ctx context.Context, ownerUserID, channelID int) httpcommon.AdminResult {
	ch, err := a.GetChannelSecret(ctx, ownerUserID, channelID)
	if err != nil {
		return httpcommon.Result(nil, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(ch.BaseURL, "/")+"/v1/models", nil)
	if err != nil {
		return httpcommon.Handled(map[string]any{"ok": false, "error": err.Error()})
	}
	if ch.AuthType == "bearer" && ch.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return httpcommon.Handled(map[string]any{"ok": false, "error": err.Error()})
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return httpcommon.Handled(map[string]any{"ok": false, "error": fmt.Sprintf("upstream status %d", res.StatusCode)})
	}
	var body struct {
		Data   []map[string]any `json:"data"`
		Models []map[string]any `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return httpcommon.Handled(map[string]any{"ok": false, "error": err.Error()})
	}
	models := body.Models
	if models == nil {
		models = body.Data
	}
	return httpcommon.Handled(map[string]any{"ok": true, "models": models})
}
