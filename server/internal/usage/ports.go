package usage

import "context"

// UsageStore records and queries request usage logs, and aggregates statistics
// from them. Statistics are computed from usage_logs in real time; there is no
// daily_usage_stats table because it was unused and its composite primary key
// could not represent global per-day rows.
type Port interface {
	ListUsageLogs(ctx context.Context, ownerUserID int, filter UsageLogFilter) (ListResponse[UsageLogDTO], error)
	GetUsageLog(ctx context.Context, ownerUserID, id int) (UsageLogDTO, error)
	InsertUsageLog(ctx context.Context, in UsageLogInput) (int, error)
	StatsOverview(ctx context.Context, ownerUserID int, startTime, endTime string) (StatsOverviewDTO, error)
	StatsDaily(ctx context.Context, ownerUserID int, dateFrom, dateTo string, page, pageSize int) (ListResponse[StatsDailyDTO], error)
	StatsChannels(ctx context.Context, ownerUserID int, startTime, endTime string) (ListResponse[StatsChannelDTO], error)
	StatsTTFT(ctx context.Context, ownerUserID int, filter TTFTStatsFilter) (TTFTStatsDTO, error)
	AggregateUsage(ctx context.Context, ownerUserID int, filter UsageAggregateFilter) (ListResponse[UsageAggregateDTO], error)

	// CountRequestsSince counts request attempts matching the filter since an
	// RFC3339 timestamp. It counts all attempts, including failures, for
	// window-based rate limiting.
	CountRequestsSince(ctx context.Context, filter UsageCountFilter) (int, error)
	CountTokensSince(ctx context.Context, filter TokenCountFilter) (int64, error)
}
