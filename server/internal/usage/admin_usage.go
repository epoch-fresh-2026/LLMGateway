package usage

import (
	"net/http"
	"strconv"
	"time"

	"LLMGateway/server/internal/httpcommon"
)

const (
	defaultStartTime = "1970-01-01T00:00:00Z"
	defaultEndTime   = "9999-12-31T23:59:59Z"
	defaultDateFrom  = "1970-01-01"
	defaultDateTo    = "9999-12-31"
)

func (a *Server) RegisterAdminRoutes(mux *http.ServeMux) {
	httpcommon.HandleAdmin(mux, "/admin/usage-logs", a.usageLogs)
	httpcommon.HandleAdmin(mux, "/admin/usage-logs/{id}", a.usageLog)
	httpcommon.HandleAdmin(mux, "/admin/stats/overview", a.statsOverview)
	httpcommon.HandleAdmin(mux, "/admin/stats/daily", a.statsDaily)
	httpcommon.HandleAdmin(mux, "/admin/stats/channels", a.statsChannels)
	httpcommon.HandleAdmin(mux, "/admin/stats/ttft", a.statsTTFT)
	httpcommon.HandleAdmin(mux, "/admin/stats/usage", a.aggregateUsage)
}

func (a *Server) usageLogs(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	return a.listUsageLogs(r)
}

func (a *Server) usageLog(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid log id")
	}
	return httpcommon.Result(a.store.GetUsageLog(r.Context(), ownerUserID, id))
}

func (a *Server) listUsageLogs(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	query := r.URL.Query()
	page, pageSize := httpcommon.ParsePagination(r)

	filter := UsageLogFilter{
		Model:     query.Get("model"),
		Status:    query.Get("status"),
		StartTime: query.Get("start_time"),
		EndTime:   query.Get("end_time"),
		Page:      page,
		PageSize:  pageSize,
	}
	if value := query.Get("user_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid user_id")
		}
		filter.UserID = &id
	}
	if value := query.Get("channel_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid channel_id")
		}
		filter.ChannelID = &id
	}
	if value := query.Get("api_key_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid api_key_id")
		}
		filter.APIKeyID = &id
	}
	return httpcommon.Result(a.store.ListUsageLogs(r.Context(), ownerUserID, filter))
}

func (a *Server) statsOverview(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	query := r.URL.Query()
	return httpcommon.Result(a.store.StatsOverview(r.Context(), ownerUserID, orDefault(query.Get("start_time"), defaultStartTime), orDefault(query.Get("end_time"), defaultEndTime)))
}

func (a *Server) statsDaily(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	query := r.URL.Query()
	page, pageSize := httpcommon.ParsePagination(r)
	return httpcommon.Result(a.store.StatsDaily(r.Context(), ownerUserID, orDefault(query.Get("date_from"), defaultDateFrom), orDefault(query.Get("date_to"), defaultDateTo), page, pageSize))
}

func (a *Server) statsChannels(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	query := r.URL.Query()
	return httpcommon.Result(a.store.StatsChannels(r.Context(), ownerUserID, orDefault(query.Get("start_time"), defaultStartTime), orDefault(query.Get("end_time"), defaultEndTime)))
}

func (a *Server) statsTTFT(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	query := r.URL.Query()
	filter := TTFTStatsFilter{Model: query.Get("model"), StartTime: orDefault(query.Get("start_time"), defaultStartTime), EndTime: orDefault(query.Get("end_time"), defaultEndTime)}
	for name, target := range map[string]**int{"user_id": &filter.UserID, "api_key_id": &filter.APIKeyID, "channel_id": &filter.ChannelID} {
		if value := query.Get(name); value != "" {
			id, err := strconv.Atoi(value)
			if err != nil {
				return httpcommon.HTTPError(http.StatusBadRequest, "invalid "+name)
			}
			*target = &id
		}
	}
	return httpcommon.Result(a.store.StatsTTFT(r.Context(), ownerUserID, filter))
}

func (a *Server) aggregateUsage(r *http.Request) httpcommon.AdminResult {
	ownerUserID, result := httpcommon.RequireUserID(r)
	if result.Status != 0 {
		return result
	}
	query := r.URL.Query()
	groupBy := query.Get("group_by")
	if groupBy != "user" && groupBy != "api_key" && groupBy != "model" && groupBy != "channel" {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid group_by")
	}
	if (query.Get("start_time") != "" || query.Get("end_time") != "") && (query.Get("date_from") != "" || query.Get("date_to") != "") {
		return httpcommon.HTTPError(http.StatusBadRequest, "time and date ranges cannot be combined")
	}
	startTime, endTime := query.Get("start_time"), query.Get("end_time")
	if query.Get("date_from") != "" || query.Get("date_to") != "" {
		if err := ValidateDateRange(query.Get("date_from"), query.Get("date_to")); err != nil {
			return httpcommon.HTTPError(http.StatusBadRequest, "invalid date range")
		}
		startTime = orDefault(query.Get("date_from"), defaultDateFrom) + "T00:00:00Z"
		if query.Get("date_to") == "" {
			endTime = defaultEndTime
		} else {
			end, _ := time.Parse("2006-01-02", query.Get("date_to"))
			endTime = end.AddDate(0, 0, 1).Format(time.RFC3339)
		}
	}
	filter := UsageAggregateFilter{GroupBy: groupBy, Model: query.Get("model"), Status: query.Get("status"), StartTime: orDefault(startTime, defaultStartTime), EndTime: orDefault(endTime, defaultEndTime)}
	filter.Page, filter.PageSize = httpcommon.ParsePagination(r)
	for name, target := range map[string]**int{"user_id": &filter.UserID, "api_key_id": &filter.APIKeyID, "channel_id": &filter.ChannelID} {
		if value := query.Get(name); value != "" {
			id, err := strconv.Atoi(value)
			if err != nil || id <= 0 {
				return httpcommon.HTTPError(http.StatusBadRequest, "invalid "+name)
			}
			*target = &id
		}
	}
	return httpcommon.Result(a.store.AggregateUsage(r.Context(), ownerUserID, filter))
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
