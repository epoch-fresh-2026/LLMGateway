package storefake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"LLMGateway/server/internal/money"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/usage"
)

func (s *Store) InsertUsageLog(_ context.Context, in domain.UsageLogInput) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insertUsageLogLocked(in)
}

func (s *Store) insertUsageLogLocked(in domain.UsageLogInput) (int, error) {
	for _, log := range s.usageLogs {
		if log.RequestID == in.RequestID {
			return 0, fmt.Errorf("%w: duplicate request_id", store.ErrInvalid)
		}
	}

	log := domain.UsageLog{
		ID:                   s.nextUsageLogID,
		RequestID:            in.RequestID,
		UserID:               in.UserID,
		APIKeyID:             in.APIKeyID,
		ChannelID:            in.ChannelID,
		ChannelName:          s.channelNameLocked(in.ChannelID),
		Model:                in.Model,
		UpstreamModel:        in.UpstreamModel,
		InputTokens:          in.InputTokens,
		OutputTokens:         in.OutputTokens,
		CachedInputTokens:    in.CachedInputTokens,
		TotalTokens:          in.TotalTokens,
		UnitPriceInputPer1M:  orZero8(in.UnitPriceInputPer1M),
		UnitPriceOutputPer1M: orZero8(in.UnitPriceOutputPer1M),
		TotalCost:            orZero6(in.TotalCost),
		DurationMs:           in.DurationMs,
		TTFTMs:               in.TTFTMs,
		Status:               in.Status,
		ErrorCode:            in.ErrorCode,
		ClientIP:             in.ClientIP,
		CreatedAt:            nowRFC3339(),
	}
	s.nextUsageLogID++
	s.usageLogs = append(s.usageLogs, log)
	return log.ID, nil
}

func (s *Store) ListUsageLogs(_ context.Context, ownerUserID int, filter domain.UsageLogFilter) (domain.ListResponse[domain.UsageLogDTO], error) {
	if err := domain.ValidateTimeRange(filter.StartTime, filter.EndTime); err != nil {
		return domain.ListResponse[domain.UsageLogDTO]{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.filterUsageLogsLocked(ownerUserID, filter)
	if err != nil {
		return domain.ListResponse[domain.UsageLogDTO]{}, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt != rows[j].CreatedAt {
			return rows[i].CreatedAt > rows[j].CreatedAt
		}
		return rows[i].ID > rows[j].ID
	})

	start, end := pageBounds(len(rows), filter.Page, filter.PageSize)
	list := []domain.UsageLogDTO{}
	for _, log := range rows[start:end] {
		list = append(list, usageLogDTO(&log))
	}
	return domain.ListResponse[domain.UsageLogDTO]{List: list, Total: len(rows)}, nil
}

func (s *Store) GetUsageLog(_ context.Context, ownerUserID, id int) (domain.UsageLogDTO, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.usageLogs {
		log := &s.usageLogs[i]
		if log.ID == id && log.UserID != nil && *log.UserID == ownerUserID {
			return usageLogDTO(log), nil
		}
	}
	return domain.UsageLogDTO{}, store.ErrNotFound
}

func (s *Store) CountRequestsSince(_ context.Context, filter domain.UsageCountFilter) (int, error) {
	if err := domain.ValidateSince(filter.Since); err != nil {
		return 0, err
	}
	sinceTime, _ := time.Parse(time.RFC3339, filter.Since)

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, log := range s.usageLogs {
		if filter.UserID > 0 && (log.UserID == nil || *log.UserID != filter.UserID) {
			continue
		}
		if filter.APIKeyID != nil {
			if log.APIKeyID == nil || *log.APIKeyID != *filter.APIKeyID {
				continue
			}
		}
		if filter.Model != "" && log.Model != filter.Model {
			continue
		}
		if filter.ChannelID != nil {
			if log.ChannelID == nil || *log.ChannelID != *filter.ChannelID {
				continue
			}
		}
		created, err := time.Parse(time.RFC3339, log.CreatedAt)
		if err != nil || created.Before(sinceTime) {
			continue
		}
		count++
	}
	return count, nil
}

func (s *Store) CountTokensSince(_ context.Context, filter domain.TokenCountFilter) (int64, error) {
	if err := domain.ValidateSince(filter.Since); err != nil {
		return 0, err
	}
	since, _ := time.Parse(time.RFC3339, filter.Since)
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, log := range s.usageLogs {
		if (filter.UserID > 0 && (log.UserID == nil || *log.UserID != filter.UserID)) || (filter.APIKeyID != nil && (log.APIKeyID == nil || *log.APIKeyID != *filter.APIKeyID)) || (filter.Model != "" && log.Model != filter.Model) || (filter.ChannelID != nil && (log.ChannelID == nil || *log.ChannelID != *filter.ChannelID)) {
			continue
		}
		created, err := time.Parse(time.RFC3339, log.CreatedAt)
		if err == nil && !created.Before(since) && domain.IsSettledUsage(log.Status, log.ErrorCode) {
			total += int64(log.TotalTokens)
		}
	}
	return total, nil
}

func (s *Store) StatsOverview(_ context.Context, ownerUserID int, startTime, endTime string) (domain.StatsOverviewDTO, error) {
	start, end, err := parseRange(startTime, endTime)
	if err != nil {
		return domain.StatsOverviewDTO{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var requests, success, errors, tokens, actual, estimated int
	cost := money.Amount(0)
	for _, log := range s.usageLogs {
		if log.UserID == nil || *log.UserID != ownerUserID {
			continue
		}
		if !withinConsumptionRange(log.CreatedAt, start, end) {
			continue
		}
		requests++
		if log.Status == "success" {
			success++
		} else {
			errors++
		}
		if domain.IsEstimatedUsage(log.Status, log.ErrorCode) {
			estimated += log.TotalTokens
		}
		if !domain.IsSettledUsage(log.Status, log.ErrorCode) {
			continue
		}
		actual += log.TotalTokens
		tokens += log.TotalTokens
		if parsed, err := money.Parse6(log.TotalCost); err == nil {
			cost = cost.Add(parsed)
		}
	}

	return domain.StatsOverviewDTO{RequestCount: int64(requests), SuccessCount: int64(success), ErrorCount: int64(errors), TotalTokens: int64(tokens), ActualTokens: int64(actual), EstimatedTokens: int64(estimated), TotalCost: money.Format6(cost), ActiveKeyCount: s.activeKeyCountLocked(ownerUserID)}, nil
}

// activeKeyCountLocked counts the owner's active, unexpired gateway keys.
func (s *Store) activeKeyCountLocked(ownerUserID int) int64 {
	now := s.now()
	var count int64
	for _, key := range s.keys {
		if key.userID != ownerUserID || !key.isActive {
			continue
		}
		if key.expiresAt != nil {
			parsed, err := time.Parse(time.RFC3339, *key.expiresAt)
			if err != nil || !parsed.After(now) {
				continue
			}
		}
		count++
	}
	return count
}

func (s *Store) StatsDaily(_ context.Context, ownerUserID int, dateFrom, dateTo string, page, pageSize int) (domain.ListResponse[domain.StatsDailyDTO], error) {
	if err := domain.ValidateDateRange(dateFrom, dateTo); err != nil {
		return domain.ListResponse[domain.StatsDailyDTO]{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	type bucket struct {
		requests, success, errors, tokens, actual, estimated int
		input, output, cached                                int
		cost                                                 money.Amount
	}
	byDate := map[string]*bucket{}
	for _, log := range s.usageLogs {
		if log.UserID == nil || *log.UserID != ownerUserID {
			continue
		}
		date := utcDate(log.CreatedAt)
		if date == "" || date < dateFrom || date > dateTo {
			continue
		}
		entry := byDate[date]
		if entry == nil {
			entry = &bucket{}
			byDate[date] = entry
		}
		entry.requests++
		if log.Status == "success" {
			entry.success++
		} else {
			entry.errors++
		}
		if domain.IsEstimatedUsage(log.Status, log.ErrorCode) {
			entry.estimated += log.TotalTokens
		}
		if !domain.IsSettledUsage(log.Status, log.ErrorCode) {
			continue
		}
		entry.actual += log.TotalTokens
		entry.tokens += log.TotalTokens
		entry.input += log.InputTokens
		entry.output += log.OutputTokens
		entry.cached += log.CachedInputTokens
		if parsed, err := money.Parse6(log.TotalCost); err == nil {
			entry.cost = entry.cost.Add(parsed)
		}
	}

	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	start, end := pageBounds(len(dates), page, pageSize)
	list := []domain.StatsDailyDTO{}
	for _, date := range dates[start:end] {
		entry := byDate[date]
		list = append(list, domain.StatsDailyDTO{StatDate: date, RequestCount: int64(entry.requests), SuccessCount: int64(entry.success), ErrorCount: int64(entry.errors), TotalTokens: int64(entry.tokens), ActualTokens: int64(entry.actual), EstimatedTokens: int64(entry.estimated), InputTokens: int64(entry.input), OutputTokens: int64(entry.output), CachedInputTokens: int64(entry.cached), TotalCost: money.Format6(entry.cost)})
	}
	return domain.ListResponse[domain.StatsDailyDTO]{List: list, Total: len(dates)}, nil
}

func (s *Store) StatsChannels(_ context.Context, ownerUserID int, startTime, endTime string) (domain.ListResponse[domain.StatsChannelDTO], error) {
	start, end, err := parseRange(startTime, endTime)
	if err != nil {
		return domain.ListResponse[domain.StatsChannelDTO]{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	type bucket struct {
		name                                                 string
		requests, success, errors, tokens, actual, estimated int
		cost                                                 money.Amount
	}
	byChannel := map[int]*bucket{}
	order := []int{}
	for _, log := range s.usageLogs {
		if log.UserID == nil || *log.UserID != ownerUserID {
			continue
		}
		if !withinConsumptionRange(log.CreatedAt, start, end) {
			continue
		}
		channelID := 0
		if log.ChannelID != nil {
			channelID = *log.ChannelID
		}
		entry := byChannel[channelID]
		if entry == nil {
			entry = &bucket{name: s.channelNameLocked(log.ChannelID)}
			byChannel[channelID] = entry
			order = append(order, channelID)
		}
		entry.requests++
		if log.Status == "success" {
			entry.success++
		} else {
			entry.errors++
		}
		if domain.IsEstimatedUsage(log.Status, log.ErrorCode) {
			entry.estimated += log.TotalTokens
		}
		if !domain.IsSettledUsage(log.Status, log.ErrorCode) {
			continue
		}
		entry.actual += log.TotalTokens
		entry.tokens += log.TotalTokens
		if parsed, err := money.Parse6(log.TotalCost); err == nil {
			entry.cost = entry.cost.Add(parsed)
		}
	}

	sort.Slice(order, func(i, j int) bool {
		if byChannel[order[i]].requests != byChannel[order[j]].requests {
			return byChannel[order[i]].requests > byChannel[order[j]].requests
		}
		return order[i] < order[j]
	})

	list := []domain.StatsChannelDTO{}
	for _, channelID := range order {
		entry := byChannel[channelID]
		list = append(list, domain.StatsChannelDTO{ChannelID: channelID, ChannelName: entry.name, RequestCount: int64(entry.requests), SuccessCount: int64(entry.success), ErrorCount: int64(entry.errors), TotalTokens: int64(entry.tokens), ActualTokens: int64(entry.actual), EstimatedTokens: int64(entry.estimated), TotalCost: money.Format6(entry.cost)})
	}
	return domain.ListResponse[domain.StatsChannelDTO]{List: list}, nil
}

func (s *Store) StatsTTFT(_ context.Context, ownerUserID int, filter domain.TTFTStatsFilter) (domain.TTFTStatsDTO, error) {
	start, end, err := parseRange(filter.StartTime, filter.EndTime)
	if err != nil {
		return domain.TTFTStatsDTO{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var values []int
	for _, log := range s.usageLogs {
		if log.UserID == nil || *log.UserID != ownerUserID {
			continue
		}
		if log.TTFTMs == nil || !withinRange(log.CreatedAt, start, end) ||
			(filter.UserID != nil && (log.UserID == nil || *log.UserID != *filter.UserID)) ||
			(filter.APIKeyID != nil && (log.APIKeyID == nil || *log.APIKeyID != *filter.APIKeyID)) ||
			(filter.ChannelID != nil && (log.ChannelID == nil || *log.ChannelID != *filter.ChannelID)) ||
			(filter.Model != "" && log.Model != filter.Model) {
			continue
		}
		values = append(values, *log.TTFTMs)
	}
	if len(values) == 0 {
		return domain.TTFTStatsDTO{}, nil
	}
	sort.Ints(values)
	var sum int64
	for _, value := range values {
		sum += int64(value)
	}
	return domain.TTFTStatsDTO{SampleCount: int64(len(values)), AverageMs: sum / int64(len(values)), P50Ms: percentile(values, 50), P95Ms: percentile(values, 95), P99Ms: percentile(values, 99)}, nil
}

func (s *Store) AggregateUsage(_ context.Context, ownerUserID int, filter domain.UsageAggregateFilter) (domain.ListResponse[domain.UsageAggregateDTO], error) {
	start, end, err := parseRange(filter.StartTime, filter.EndTime)
	if err != nil {
		return domain.ListResponse[domain.UsageAggregateDTO]{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byGroup := map[string]*domain.UsageAggregateDTO{}
	for _, log := range s.usageLogs {
		if log.UserID == nil || *log.UserID != ownerUserID {
			continue
		}
		created, err := time.Parse(time.RFC3339, log.CreatedAt)
		if err != nil || (!start.IsZero() && created.Before(start)) || (!end.IsZero() && !created.Before(end)) || (filter.UserID != nil && (log.UserID == nil || *log.UserID != *filter.UserID)) || (filter.APIKeyID != nil && (log.APIKeyID == nil || *log.APIKeyID != *filter.APIKeyID)) || (filter.ChannelID != nil && (log.ChannelID == nil || *log.ChannelID != *filter.ChannelID)) || (filter.Model != "" && log.Model != filter.Model) || (filter.Status != "" && log.Status != filter.Status) {
			continue
		}
		key := ""
		item := domain.UsageAggregateDTO{}
		switch filter.GroupBy {
		case "user":
			item.UserID = log.UserID
			if log.UserID != nil {
				key = fmt.Sprintf("%d", *log.UserID)
			}
		case "api_key":
			item.APIKeyID = log.APIKeyID
			if log.APIKeyID != nil {
				key = fmt.Sprintf("%d", *log.APIKeyID)
			}
		case "model":
			item.Model = log.Model
			key = log.Model
		case "channel":
			item.ChannelID = log.ChannelID
			if log.ChannelID != nil {
				key = fmt.Sprintf("%d", *log.ChannelID)
			}
		default:
			return domain.ListResponse[domain.UsageAggregateDTO]{}, store.ErrInvalid
		}
		entry := byGroup[key]
		if entry == nil {
			entry = &item
			byGroup[key] = entry
		}
		entry.RequestCount++
		if log.Status == "success" {
			entry.SuccessCount++
		} else {
			entry.ErrorCount++
		}
		entry.DurationMs += int64(log.DurationMs)
		if entry.TotalCost == "" {
			entry.TotalCost = "0.000000"
		}
		if domain.IsEstimatedUsage(log.Status, log.ErrorCode) {
			entry.EstimatedTokens += int64(log.TotalTokens)
		}
		if !domain.IsSettledUsage(log.Status, log.ErrorCode) {
			continue
		}
		entry.TotalTokens += int64(log.TotalTokens)
		entry.ActualTokens += int64(log.TotalTokens)
		if cost, err := money.Parse6(log.TotalCost); err == nil {
			current, _ := money.Parse6(entry.TotalCost)
			entry.TotalCost = money.Format6(current.Add(cost))
		}
	}
	list := make([]domain.UsageAggregateDTO, 0, len(byGroup))
	for _, item := range byGroup {
		list = append(list, *item)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RequestCount != list[j].RequestCount {
			return list[i].RequestCount > list[j].RequestCount
		}
		return aggregateKey(list[i], filter.GroupBy) < aggregateKey(list[j], filter.GroupBy)
	})
	startIndex, endIndex := pageBounds(len(list), filter.Page, filter.PageSize)
	return domain.ListResponse[domain.UsageAggregateDTO]{List: list[startIndex:endIndex], Total: len(list)}, nil
}

func aggregateKey(item domain.UsageAggregateDTO, groupBy string) string {
	switch groupBy {
	case "user":
		if item.UserID != nil {
			return fmt.Sprintf("%d", *item.UserID)
		}
	case "api_key":
		if item.APIKeyID != nil {
			return fmt.Sprintf("%d", *item.APIKeyID)
		}
	case "model":
		return item.Model
	case "channel":
		if item.ChannelID != nil {
			return fmt.Sprintf("%d", *item.ChannelID)
		}
	}
	return ""
}

func percentile(values []int, percentile int) int64 {
	index := (len(values)*percentile + 99) / 100
	return int64(values[index-1])
}

// orZero6/orZero8 mirror the PostgreSQL NOT NULL DEFAULT 0 columns so both
// stores return the same canonical strings for missing amounts.
func orZero6(value string) string {
	if value == "" {
		return "0.000000"
	}
	if parsed, err := money.Parse6(value); err == nil {
		return money.Format6(parsed)
	}
	return value
}

func orZero8(value string) string {
	if value == "" {
		return "0.00000000"
	}
	if parsed, err := money.Parse8(value); err == nil {
		return money.Format8(parsed)
	}
	return value
}

func (s *Store) filterUsageLogsLocked(ownerUserID int, filter domain.UsageLogFilter) ([]domain.UsageLog, error) {
	start, end, err := parseRange(filter.StartTime, filter.EndTime)
	if err != nil {
		return nil, err
	}
	rows := []domain.UsageLog{}
	for _, log := range s.usageLogs {
		if log.UserID == nil || *log.UserID != ownerUserID {
			continue
		}
		if filter.UserID != nil && (log.UserID == nil || *log.UserID != *filter.UserID) {
			continue
		}
		if filter.ChannelID != nil && (log.ChannelID == nil || *log.ChannelID != *filter.ChannelID) {
			continue
		}
		if filter.APIKeyID != nil && (log.APIKeyID == nil || *log.APIKeyID != *filter.APIKeyID) {
			continue
		}
		if filter.Model != "" && log.Model != filter.Model {
			continue
		}
		if filter.Status != "" && log.Status != filter.Status {
			continue
		}
		if !withinConsumptionRange(log.CreatedAt, start, end) {
			continue
		}
		rows = append(rows, log)
	}
	return rows, nil
}

func (s *Store) channelNameLocked(channelID *int) string {
	if channelID == nil {
		return ""
	}
	if channel, ok := s.channels[*channelID]; ok {
		return channel.Name
	}
	return ""
}

func usageLogDTO(log *domain.UsageLog) domain.UsageLogDTO {
	return domain.UsageLogDTO{ID: log.ID, RequestID: log.RequestID, UserID: log.UserID, APIKeyID: log.APIKeyID, ChannelID: log.ChannelID, ChannelName: log.ChannelName, Model: log.Model, UpstreamModel: log.UpstreamModel, InputTokens: int64(log.InputTokens), OutputTokens: int64(log.OutputTokens), CachedInputTokens: int64(log.CachedInputTokens), TotalTokens: int64(log.TotalTokens), UnitPriceInputPer1M: log.UnitPriceInputPer1M, UnitPriceOutputPer1M: log.UnitPriceOutputPer1M, TotalCost: log.TotalCost, DurationMs: int64(log.DurationMs), TTFTMs: log.TTFTMs, Status: log.Status, ErrorCode: log.ErrorCode, ClientIP: log.ClientIP, CreatedAt: log.CreatedAt}
}

func parseRange(startTime, endTime string) (time.Time, time.Time, error) {
	var start, end time.Time
	var err error
	if startTime != "" {
		start, err = time.Parse(time.RFC3339, startTime)
		if err != nil {
			return start, end, store.ErrInvalid
		}
	}
	if endTime != "" {
		end, err = time.Parse(time.RFC3339, endTime)
		if err != nil {
			return start, end, store.ErrInvalid
		}
	}
	return start, end, nil
}

func withinConsumptionRange(createdAt string, start, end time.Time) bool {
	created, err := time.Parse(time.RFC3339, createdAt)
	return err == nil && (start.IsZero() || !created.Before(start)) && (end.IsZero() || created.Before(end))
}

func withinRange(createdAt string, start, end time.Time) bool {
	if start.IsZero() && end.IsZero() {
		return true
	}
	created, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return false
	}
	if !start.IsZero() && created.Before(start) {
		return false
	}
	if !end.IsZero() && created.After(end) {
		return false
	}
	return true
}

func utcDate(createdAt string) string {
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format("2006-01-02")
}
