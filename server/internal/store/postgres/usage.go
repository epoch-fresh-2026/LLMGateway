package postgres

import (
	"context"
	"time"

	"LLMGateway/server/internal/db/sqlc"
	"LLMGateway/server/internal/store"
	domain "LLMGateway/server/internal/usage"

	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) ListUsageLogs(ctx context.Context, ownerUserID int, filter domain.UsageLogFilter) (domain.ListResponse[domain.UsageLogDTO], error) {
	if err := domain.ValidateTimeRange(filter.StartTime, filter.EndTime); err != nil {
		return domain.ListResponse[domain.UsageLogDTO]{}, err
	}
	limit, offset := limitOffset(filter.Page, filter.PageSize)
	params := sqlc.ListUsageLogsParams{
		OwnerUserID: int8Value(&ownerUserID),
		UserID:      int8Value(filter.UserID),
		ApiKeyID:    int8Value(filter.APIKeyID),
		ChannelID:   int8Value(filter.ChannelID),
		Model:       textValueParam(filter.Model),
		Status:      textValueParam(filter.Status),
		StartTime:   timestampValue(filter.StartTime),
		EndTime:     timestampValue(filter.EndTime),
		PageOffset:  offset,
		PageLimit:   limit,
	}

	rows, err := s.queries.ListUsageLogs(ctx, params)
	if err != nil {
		return domain.ListResponse[domain.UsageLogDTO]{}, mapError(err)
	}
	total, err := s.queries.CountUsageLogs(ctx, sqlc.CountUsageLogsParams{
		OwnerUserID: params.OwnerUserID,
		UserID:      params.UserID,
		ApiKeyID:    params.ApiKeyID,
		ChannelID:   params.ChannelID,
		Model:       params.Model,
		Status:      params.Status,
		StartTime:   params.StartTime,
		EndTime:     params.EndTime,
	})
	if err != nil {
		return domain.ListResponse[domain.UsageLogDTO]{}, mapError(err)
	}

	list := []domain.UsageLogDTO{}
	for _, row := range rows {
		list = append(list, usageLogDTO(row.ID, row.RequestID, row.UserID, row.ApiKeyID, row.ChannelID, row.ChannelName, row.Model, row.UpstreamModel, row.InputTokens, row.OutputTokens, row.CachedInputTokens, row.TotalTokens, row.UnitPriceInputPer1m, row.UnitPriceOutputPer1m, row.TotalCost, row.DurationMs, row.TtftMs, row.Status, row.ErrorCode, row.ClientIp, row.CreatedAt))
	}
	return domain.ListResponse[domain.UsageLogDTO]{List: list, Total: int(total)}, nil
}

func (s *Store) GetUsageLog(ctx context.Context, ownerUserID, id int) (domain.UsageLogDTO, error) {
	row, err := s.queries.GetUsageLog(ctx, sqlc.GetUsageLogParams{ID: int64(id), OwnerUserID: int8Value(&ownerUserID)})
	if err != nil {
		return domain.UsageLogDTO{}, mapError(err)
	}
	return usageLogDTO(row.ID, row.RequestID, row.UserID, row.ApiKeyID, row.ChannelID, row.ChannelName, row.Model, row.UpstreamModel, row.InputTokens, row.OutputTokens, row.CachedInputTokens, row.TotalTokens, row.UnitPriceInputPer1m, row.UnitPriceOutputPer1m, row.TotalCost, row.DurationMs, row.TtftMs, row.Status, row.ErrorCode, row.ClientIp, row.CreatedAt), nil
}

func (s *Store) InsertUsageLog(ctx context.Context, in domain.UsageLogInput) (int, error) {
	id, err := insertUsageLog(ctx, s.queries, in)
	if err != nil {
		return 0, mapError(err)
	}
	return id, nil
}

func insertUsageLog(ctx context.Context, queries *sqlc.Queries, in domain.UsageLogInput) (int, error) {
	id, err := queries.InsertUsageLog(ctx, sqlc.InsertUsageLogParams{
		RequestID:            in.RequestID,
		UserID:               int8Value(in.UserID),
		ApiKeyID:             int8Value(in.APIKeyID),
		ChannelID:            int8Value(in.ChannelID),
		Model:                in.Model,
		UpstreamModel:        in.UpstreamModel,
		InputTokens:          int64(in.InputTokens),
		OutputTokens:         int64(in.OutputTokens),
		CachedInputTokens:    int64(in.CachedInputTokens),
		TotalTokens:          int64(in.TotalTokens),
		UnitPriceInputPer1m:  in.UnitPriceInputPer1M,
		UnitPriceOutputPer1m: in.UnitPriceOutputPer1M,
		TotalCost:            in.TotalCost,
		DurationMs:           int64(in.DurationMs),
		TtftMs:               int8Value(in.TTFTMs),
		Status:               in.Status,
		ErrorCode:            in.ErrorCode,
		ClientIp:             in.ClientIP,
	})
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

func (s *Store) CountRequestsSince(ctx context.Context, filter domain.UsageCountFilter) (int, error) {
	if err := domain.ValidateSince(filter.Since); err != nil {
		return 0, err
	}
	count, err := s.queries.CountRequestsSince(ctx, sqlc.CountRequestsSinceParams{
		UserID:    int8Value(&filter.UserID),
		Since:     timestampValue(filter.Since),
		ApiKeyID:  int8Value(filter.APIKeyID),
		Model:     textValueParam(filter.Model),
		ChannelID: int8Value(filter.ChannelID),
	})
	if err != nil {
		return 0, mapError(err)
	}
	return int(count), nil
}

func (s *Store) CountTokensSince(ctx context.Context, filter domain.TokenCountFilter) (int64, error) {
	if err := domain.ValidateSince(filter.Since); err != nil {
		return 0, err
	}
	total, err := s.queries.CountTokensSince(ctx, sqlc.CountTokensSinceParams{
		UserID:    int64(filter.UserID),
		Since:     timestampValue(filter.Since),
		ApiKeyID:  int64(optionalID(filter.APIKeyID)),
		Model:     filter.Model,
		ChannelID: int64(optionalID(filter.ChannelID)),
	})
	if err != nil {
		return 0, mapError(err)
	}
	return total, nil
}

func optionalID(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Store) StatsOverview(ctx context.Context, ownerUserID int, startTime, endTime string) (domain.StatsOverviewDTO, error) {
	if err := domain.ValidateTimeRange(startTime, endTime); err != nil {
		return domain.StatsOverviewDTO{}, err
	}
	row, err := s.queries.StatsOverview(ctx, sqlc.StatsOverviewParams{
		OwnerUserID: int64(ownerUserID),
		StartTime:   timestampValue(startTime),
		EndTime:     timestampValue(endTime),
	})
	if err != nil {
		return domain.StatsOverviewDTO{}, mapError(err)
	}
	return domain.StatsOverviewDTO{RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, TotalCost: row.TotalCost, ActiveKeyCount: row.ActiveKeyCount}, nil
}

func (s *Store) StatsDaily(ctx context.Context, ownerUserID int, dateFrom, dateTo string, page, pageSize int) (domain.ListResponse[domain.StatsDailyDTO], error) {
	if err := domain.ValidateDateRange(dateFrom, dateTo); err != nil {
		return domain.ListResponse[domain.StatsDailyDTO]{}, err
	}
	limit, offset := limitOffset(page, pageSize)

	rows, err := s.queries.StatsDaily(ctx, sqlc.StatsDailyParams{OwnerUserID: int8Value(&ownerUserID), DateFrom: dateFrom, DateTo: dateTo, PageOffset: offset, PageLimit: limit})
	if err != nil {
		return domain.ListResponse[domain.StatsDailyDTO]{}, mapError(err)
	}
	total, err := s.queries.CountStatsDaily(ctx, sqlc.CountStatsDailyParams{OwnerUserID: int8Value(&ownerUserID), DateFrom: dateFrom, DateTo: dateTo})
	if err != nil {
		return domain.ListResponse[domain.StatsDailyDTO]{}, mapError(err)
	}

	list := []domain.StatsDailyDTO{}
	for _, row := range rows {
		list = append(list, domain.StatsDailyDTO{StatDate: row.StatDate.Time.UTC().Format("2006-01-02"), RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, CachedInputTokens: row.CachedInputTokens, TotalCost: row.TotalCost})
	}
	return domain.ListResponse[domain.StatsDailyDTO]{List: list, Total: int(total)}, nil
}

func (s *Store) StatsChannels(ctx context.Context, ownerUserID int, startTime, endTime string) (domain.ListResponse[domain.StatsChannelDTO], error) {
	if err := domain.ValidateTimeRange(startTime, endTime); err != nil {
		return domain.ListResponse[domain.StatsChannelDTO]{}, err
	}
	rows, err := s.queries.StatsChannels(ctx, sqlc.StatsChannelsParams{
		OwnerUserID: int8Value(&ownerUserID),
		StartTime:   timestampValue(startTime),
		EndTime:     timestampValue(endTime),
	})
	if err != nil {
		return domain.ListResponse[domain.StatsChannelDTO]{}, mapError(err)
	}

	list := []domain.StatsChannelDTO{}
	for _, row := range rows {
		channelID := 0
		if row.ChannelID.Valid {
			channelID = int(row.ChannelID.Int64)
		}
		list = append(list, domain.StatsChannelDTO{ChannelID: channelID, ChannelName: row.ChannelName, RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, TotalCost: row.TotalCost})
	}
	return domain.ListResponse[domain.StatsChannelDTO]{List: list}, nil
}

func (s *Store) StatsTTFT(ctx context.Context, ownerUserID int, filter domain.TTFTStatsFilter) (domain.TTFTStatsDTO, error) {
	if err := domain.ValidateTimeRange(filter.StartTime, filter.EndTime); err != nil {
		return domain.TTFTStatsDTO{}, err
	}
	row, err := s.queries.StatsTTFT(ctx, sqlc.StatsTTFTParams{
		OwnerUserID: int8Value(&ownerUserID),
		UserID:      int8Value(filter.UserID),
		ApiKeyID:    int8Value(filter.APIKeyID),
		ChannelID:   int8Value(filter.ChannelID),
		Model:       textValueParam(filter.Model),
		StartTime:   timestampValue(filter.StartTime),
		EndTime:     timestampValue(filter.EndTime),
	})
	if err != nil {
		return domain.TTFTStatsDTO{}, mapError(err)
	}
	return domain.TTFTStatsDTO{SampleCount: row.SampleCount, AverageMs: row.AverageMs, P50Ms: row.P50Ms, P95Ms: row.P95Ms, P99Ms: row.P99Ms}, nil
}

func (s *Store) AggregateUsage(ctx context.Context, ownerUserID int, filter domain.UsageAggregateFilter) (domain.ListResponse[domain.UsageAggregateDTO], error) {
	if err := domain.ValidateTimeRange(filter.StartTime, filter.EndTime); err != nil {
		return domain.ListResponse[domain.UsageAggregateDTO]{}, err
	}
	limit, offset := limitOffset(filter.Page, filter.PageSize)
	params := sqlc.AggregateUsageByModelParams{OwnerUserID: int8Value(&ownerUserID), StartTime: timestampValue(filter.StartTime), EndTime: timestampValue(filter.EndTime), UserID: int8Value(filter.UserID), ApiKeyID: int8Value(filter.APIKeyID), ChannelID: int8Value(filter.ChannelID), Model: textValueParam(filter.Model), Status: textValueParam(filter.Status), PageOffset: offset, PageLimit: limit}
	list := []domain.UsageAggregateDTO{}
	total := 0
	switch filter.GroupBy {
	case "user":
		rows, err := s.queries.AggregateUsageByUser(ctx, sqlc.AggregateUsageByUserParams(params))
		if err != nil {
			return domain.ListResponse[domain.UsageAggregateDTO]{}, mapError(err)
		}
		for _, row := range rows {
			list = append(list, domain.UsageAggregateDTO{UserID: optionalInt(row.UserID), RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, TotalCost: row.TotalCost, DurationMs: row.DurationMs})
			total = int(row.AggregateTotal)
		}
	case "api_key":
		rows, err := s.queries.AggregateUsageByAPIKey(ctx, sqlc.AggregateUsageByAPIKeyParams(params))
		if err != nil {
			return domain.ListResponse[domain.UsageAggregateDTO]{}, mapError(err)
		}
		for _, row := range rows {
			list = append(list, domain.UsageAggregateDTO{APIKeyID: optionalInt(row.ApiKeyID), RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, TotalCost: row.TotalCost, DurationMs: row.DurationMs})
			total = int(row.AggregateTotal)
		}
	case "model":
		rows, err := s.queries.AggregateUsageByModel(ctx, params)
		if err != nil {
			return domain.ListResponse[domain.UsageAggregateDTO]{}, mapError(err)
		}
		for _, row := range rows {
			list = append(list, domain.UsageAggregateDTO{Model: row.Model, RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, TotalCost: row.TotalCost, DurationMs: row.DurationMs})
			total = int(row.AggregateTotal)
		}
	case "channel":
		rows, err := s.queries.AggregateUsageByChannel(ctx, sqlc.AggregateUsageByChannelParams(params))
		if err != nil {
			return domain.ListResponse[domain.UsageAggregateDTO]{}, mapError(err)
		}
		for _, row := range rows {
			list = append(list, domain.UsageAggregateDTO{ChannelID: optionalInt(row.ChannelID), RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, ErrorCount: row.ErrorCount, TotalTokens: row.TotalTokens, ActualTokens: row.ActualTokens, EstimatedTokens: row.EstimatedTokens, TotalCost: row.TotalCost, DurationMs: row.DurationMs})
			total = int(row.AggregateTotal)
		}
	default:
		return domain.ListResponse[domain.UsageAggregateDTO]{}, store.ErrInvalid
	}
	return domain.ListResponse[domain.UsageAggregateDTO]{List: list, Total: total}, nil
}

func usageLogDTO(id int64, requestID string, userID, apiKeyID, channelID pgtype.Int8, channelName pgtype.Text, model, upstreamModel string, inputTokens, outputTokens, cachedInputTokens, totalTokens int64, unitPriceInput, unitPriceOutput, totalCost string, durationMs int64, ttftMs pgtype.Int8, status, errorCode, clientIP string, createdAt pgtype.Timestamptz) domain.UsageLogDTO {
	return domain.UsageLogDTO{ID: int(id), RequestID: requestID, UserID: optionalInt(userID), APIKeyID: optionalInt(apiKeyID), ChannelID: optionalInt(channelID), ChannelName: textOrEmpty(channelName), Model: model, UpstreamModel: upstreamModel, InputTokens: inputTokens, OutputTokens: outputTokens, CachedInputTokens: cachedInputTokens, TotalTokens: totalTokens, UnitPriceInputPer1M: unitPriceInput, UnitPriceOutputPer1M: unitPriceOutput, TotalCost: totalCost, DurationMs: durationMs, TTFTMs: optionalInt(ttftMs), Status: status, ErrorCode: errorCode, ClientIP: clientIP, CreatedAt: createdAt.Time.UTC().Format(time.RFC3339)}
}

func textValueParam(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}
