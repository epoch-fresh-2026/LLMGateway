package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"LLMGateway/server/internal/httpcommon"
)

const channelTestTimeout = 10 * time.Second

func (a *Server) testChannel(r *http.Request, ownerUserID, channelID int) httpcommon.AdminResult {
	var request struct {
		CheckAll *bool `json:"check_all"`
	}
	if err := httpcommon.ReadJSON(r, &request); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	checkAll := request.CheckAll == nil || *request.CheckAll
	channel, err := a.GetChannelSecret(r.Context(), ownerUserID, channelID)
	if err != nil {
		return httpcommon.Result(nil, err)
	}
	models, err := a.ListChannelModels(r.Context(), ownerUserID, channelID)
	if err != nil {
		return httpcommon.Result(nil, err)
	}
	sort.Slice(models.List, func(i, j int) bool { return models.List[i].ID < models.List[j].ID })
	items := []ChannelTestItemDTO{}
	for _, model := range models.List {
		if !model.Enabled {
			continue
		}
		items = append(items, a.testModel(r.Context(), channel, model))
		if !checkAll {
			break
		}
	}
	return httpcommon.Handled(ChannelTestResultDTO{List: items})
}

// TestChannel exposes the admin channel probe to external package tests.
func (a *Server) TestChannel(r *http.Request, ownerUserID, channelID int) httpcommon.AdminResult {
	return a.testChannel(r, ownerUserID, channelID)
}

// ConfigureTestTimeout changes the probe timeout for deterministic tests.
func (a *Server) ConfigureTestTimeout(timeout time.Duration) {
	if timeout > 0 {
		a.testTimeout = timeout
	}
}

func (a *Server) testModel(parent context.Context, channel *Channel, model ChannelModel) ChannelTestItemDTO {
	item := ChannelTestItemDTO{ModelAlias: model.ModelName, UpstreamModel: model.UpstreamModel}
	// The probe payload contains only JSON-compatible strings and integers.
	body, _ := json.Marshal(map[string]any{
		"model":      model.UpstreamModel,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	})
	ctx, cancel := context.WithTimeout(parent, a.testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(channel.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		item.Error = "unable to create test request"
		return item
	}
	req.Header.Set("Content-Type", "application/json")
	if channel.AuthType == "bearer" && channel.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+channel.APIKey)
	}
	started := time.Now()
	res, err := a.client.Do(req)
	item.LatencyMs = int(time.Since(started).Milliseconds())
	if err != nil {
		if ctx.Err() != nil {
			item.Error = "upstream request timed out"
		} else {
			item.Error = "upstream request failed"
		}
		return item
	}
	defer res.Body.Close()
	item.HTTPStatus = res.StatusCode
	item.OK = res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices
	if !item.OK {
		item.Error = fmt.Sprintf("upstream status %d", res.StatusCode)
	}
	return item
}
