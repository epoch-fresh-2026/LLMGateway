-- name: ListUsageLogs :many
SELECT
    l.id,
    l.request_id,
    l.user_id,
    l.api_key_id,
    l.channel_id,
    c.name AS channel_name,
    l.model,
    l.upstream_model,
    l.input_tokens,
    l.output_tokens,
    l.cached_input_tokens,
    l.total_tokens,
    l.unit_price_input_per_1m::text AS unit_price_input_per_1m,
    l.unit_price_output_per_1m::text AS unit_price_output_per_1m,
    l.total_cost::text AS total_cost,
    l.duration_ms,
    l.ttft_ms,
    l.status,
    l.error_code,
    l.client_ip,
    l.created_at
FROM usage_logs l
LEFT JOIN channels c ON c.id = l.channel_id
WHERE l.user_id = sqlc.arg(owner_user_id)
  AND (sqlc.narg(user_id)::bigint IS NULL OR l.user_id = sqlc.narg(user_id)::bigint)
  AND (sqlc.narg(api_key_id)::bigint IS NULL OR l.api_key_id = sqlc.narg(api_key_id)::bigint)
  AND (sqlc.narg(channel_id)::bigint IS NULL OR l.channel_id = sqlc.narg(channel_id)::bigint)
  AND (sqlc.narg(model)::text IS NULL OR l.model = sqlc.narg(model)::text)
  AND (sqlc.narg(status)::text IS NULL OR l.status = sqlc.narg(status)::text)
  AND (sqlc.narg(start_time)::timestamptz IS NULL OR l.created_at >= sqlc.narg(start_time)::timestamptz)
  AND (sqlc.narg(end_time)::timestamptz IS NULL OR l.created_at <= sqlc.narg(end_time)::timestamptz)
ORDER BY l.created_at DESC, l.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountUsageLogs :one
SELECT count(*)::int
FROM usage_logs l
WHERE l.user_id = sqlc.arg(owner_user_id)
  AND (sqlc.narg(user_id)::bigint IS NULL OR l.user_id = sqlc.narg(user_id)::bigint)
  AND (sqlc.narg(api_key_id)::bigint IS NULL OR l.api_key_id = sqlc.narg(api_key_id)::bigint)
  AND (sqlc.narg(channel_id)::bigint IS NULL OR l.channel_id = sqlc.narg(channel_id)::bigint)
  AND (sqlc.narg(model)::text IS NULL OR l.model = sqlc.narg(model)::text)
  AND (sqlc.narg(status)::text IS NULL OR l.status = sqlc.narg(status)::text)
  AND (sqlc.narg(start_time)::timestamptz IS NULL OR l.created_at >= sqlc.narg(start_time)::timestamptz)
  AND (sqlc.narg(end_time)::timestamptz IS NULL OR l.created_at <= sqlc.narg(end_time)::timestamptz);

-- name: GetUsageLog :one
SELECT
    l.id,
    l.request_id,
    l.user_id,
    l.api_key_id,
    l.channel_id,
    c.name AS channel_name,
    l.model,
    l.upstream_model,
    l.input_tokens,
    l.output_tokens,
    l.cached_input_tokens,
    l.total_tokens,
    l.unit_price_input_per_1m::text AS unit_price_input_per_1m,
    l.unit_price_output_per_1m::text AS unit_price_output_per_1m,
    l.total_cost::text AS total_cost,
    l.duration_ms,
    l.ttft_ms,
    l.status,
    l.error_code,
    l.client_ip,
    l.created_at
FROM usage_logs l
LEFT JOIN channels c ON c.id = l.channel_id
WHERE l.id = sqlc.arg(id) AND l.user_id = sqlc.arg(owner_user_id);

-- name: InsertUsageLog :one
INSERT INTO usage_logs (
    request_id, user_id, api_key_id, channel_id, model, upstream_model,
    input_tokens, output_tokens, cached_input_tokens, total_tokens,
    unit_price_input_per_1m, unit_price_output_per_1m, total_cost,
    duration_ms, ttft_ms, status, error_code, client_ip
)
VALUES (
    sqlc.arg(request_id),
    sqlc.narg(user_id),
    sqlc.narg(api_key_id),
    sqlc.narg(channel_id),
    sqlc.arg(model),
    sqlc.arg(upstream_model),
    sqlc.arg(input_tokens),
    sqlc.arg(output_tokens),
    sqlc.arg(cached_input_tokens),
    sqlc.arg(total_tokens),
    COALESCE(NULLIF(sqlc.arg(unit_price_input_per_1m), '')::numeric, 0),
    COALESCE(NULLIF(sqlc.arg(unit_price_output_per_1m), '')::numeric, 0),
    COALESCE(NULLIF(sqlc.arg(total_cost), '')::numeric, 0),
    sqlc.arg(duration_ms),
    sqlc.narg(ttft_ms),
    sqlc.arg(status),
    sqlc.arg(error_code),
    sqlc.arg(client_ip)
)
RETURNING id;

-- name: CountRequestsSince :one
SELECT count(*)::int
FROM usage_logs
WHERE (sqlc.arg(user_id) = 0 OR user_id = sqlc.arg(user_id))
  AND created_at >= sqlc.arg(since)::timestamptz
  AND (sqlc.narg(api_key_id)::bigint IS NULL OR api_key_id = sqlc.narg(api_key_id)::bigint)
  AND (sqlc.narg(model)::text IS NULL OR model = sqlc.narg(model)::text)
  AND (sqlc.narg(channel_id)::bigint IS NULL OR channel_id = sqlc.narg(channel_id)::bigint);

-- name: CountTokensSince :one
SELECT COALESCE(SUM(total_tokens), 0)::bigint AS total_tokens
FROM usage_logs
WHERE (sqlc.arg(user_id)::bigint = 0 OR user_id = sqlc.arg(user_id)::bigint)
  AND created_at >= sqlc.arg(since)::timestamptz
  AND (sqlc.arg(api_key_id)::bigint = 0 OR api_key_id = sqlc.arg(api_key_id)::bigint)
  AND (sqlc.arg(model)::text = '' OR model = sqlc.arg(model)::text)
  AND (sqlc.arg(channel_id)::bigint = 0 OR channel_id = sqlc.arg(channel_id)::bigint);

-- name: StatsOverview :one
SELECT
    count(*)::bigint AS request_count,
    count(*) FILTER (WHERE status = 'success')::bigint AS success_count,
    count(*) FILTER (WHERE status <> 'success')::bigint AS error_count,
    coalesce(sum(total_tokens), 0)::bigint AS total_tokens,
    coalesce(sum(total_cost), 0)::numeric(20, 6)::text AS total_cost,
    (SELECT count(*) FROM client_api_keys k
      WHERE k.user_id = sqlc.arg(owner_user_id) AND k.is_active = true
        AND (k.expires_at IS NULL OR k.expires_at > now()))::bigint AS active_key_count
FROM usage_logs
WHERE user_id = sqlc.arg(owner_user_id)
  AND created_at >= sqlc.arg(start_time)::timestamptz
  AND created_at <= sqlc.arg(end_time)::timestamptz;

-- name: StatsDaily :many
SELECT
    (l.created_at AT TIME ZONE 'UTC')::date AS stat_date,
    count(*)::bigint AS request_count,
    count(*) FILTER (WHERE l.status = 'success')::bigint AS success_count,
    count(*) FILTER (WHERE l.status <> 'success')::bigint AS error_count,
    coalesce(sum(l.total_tokens), 0)::bigint AS total_tokens,
    coalesce(sum(l.input_tokens), 0)::bigint AS input_tokens,
    coalesce(sum(l.output_tokens), 0)::bigint AS output_tokens,
    coalesce(sum(l.cached_input_tokens), 0)::bigint AS cached_input_tokens,
    coalesce(sum(l.total_cost), 0)::numeric(20, 6)::text AS total_cost
FROM usage_logs l
WHERE l.user_id = sqlc.arg(owner_user_id)
  AND (l.created_at AT TIME ZONE 'UTC')::date >= sqlc.arg(date_from)::text::date
  AND (l.created_at AT TIME ZONE 'UTC')::date <= sqlc.arg(date_to)::text::date
GROUP BY stat_date
ORDER BY stat_date
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountStatsDaily :one
SELECT count(*)::int FROM (
    SELECT (l.created_at AT TIME ZONE 'UTC')::date AS stat_date
    FROM usage_logs l
    WHERE l.user_id = sqlc.arg(owner_user_id)
      AND (l.created_at AT TIME ZONE 'UTC')::date >= sqlc.arg(date_from)::text::date
      AND (l.created_at AT TIME ZONE 'UTC')::date <= sqlc.arg(date_to)::text::date
    GROUP BY stat_date
) AS days;

-- name: StatsChannels :many
SELECT
    l.channel_id,
    coalesce(c.name, '') AS channel_name,
    count(*)::bigint AS request_count,
    count(*) FILTER (WHERE l.status = 'success')::bigint AS success_count,
    count(*) FILTER (WHERE l.status <> 'success')::bigint AS error_count,
    coalesce(sum(l.total_tokens), 0)::bigint AS total_tokens,
    coalesce(sum(l.total_cost), 0)::numeric(20, 6)::text AS total_cost
FROM usage_logs l
JOIN channels c ON c.id = l.channel_id
WHERE l.user_id = sqlc.arg(owner_user_id)
  AND l.created_at >= sqlc.arg(start_time)::timestamptz
  AND l.created_at <= sqlc.arg(end_time)::timestamptz
GROUP BY l.channel_id, c.name
ORDER BY request_count DESC, l.channel_id;

-- name: StatsTTFT :one
SELECT
    count(*)::bigint AS sample_count,
    coalesce(floor(avg(ttft_ms))::bigint, 0)::bigint AS average_ms,
    coalesce(percentile_disc(0.50) WITHIN GROUP (ORDER BY ttft_ms), 0)::bigint AS p50_ms,
    coalesce(percentile_disc(0.95) WITHIN GROUP (ORDER BY ttft_ms), 0)::bigint AS p95_ms,
    coalesce(percentile_disc(0.99) WITHIN GROUP (ORDER BY ttft_ms), 0)::bigint AS p99_ms
FROM usage_logs
WHERE ttft_ms IS NOT NULL
  AND user_id = sqlc.arg(owner_user_id)
  AND (sqlc.narg(user_id)::bigint IS NULL OR user_id = sqlc.narg(user_id)::bigint)
  AND (sqlc.narg(api_key_id)::bigint IS NULL OR api_key_id = sqlc.narg(api_key_id)::bigint)
  AND (sqlc.narg(channel_id)::bigint IS NULL OR channel_id = sqlc.narg(channel_id)::bigint)
  AND (sqlc.narg(model)::text IS NULL OR model = sqlc.narg(model)::text)
  AND created_at >= sqlc.arg(start_time)::timestamptz
  AND created_at <= sqlc.arg(end_time)::timestamptz;

-- name: AggregateUsageByUser :many
SELECT user_id, count(*)::bigint AS request_count, count(*) FILTER (WHERE status = 'success')::bigint AS success_count, count(*) FILTER (WHERE status <> 'success')::bigint AS error_count, coalesce(sum(total_tokens), 0)::bigint AS total_tokens, coalesce(sum(total_cost), 0)::numeric(20, 6)::text AS total_cost, coalesce(sum(duration_ms), 0)::bigint AS duration_ms, count(*) OVER()::bigint AS aggregate_total
FROM usage_logs
WHERE user_id = sqlc.arg(owner_user_id) AND created_at >= sqlc.arg(start_time)::timestamptz AND created_at < sqlc.arg(end_time)::timestamptz
  AND (sqlc.narg(user_id)::bigint IS NULL OR user_id = sqlc.narg(user_id)::bigint) AND (sqlc.narg(api_key_id)::bigint IS NULL OR api_key_id = sqlc.narg(api_key_id)::bigint) AND (sqlc.narg(channel_id)::bigint IS NULL OR channel_id = sqlc.narg(channel_id)::bigint) AND (sqlc.narg(model)::text IS NULL OR model = sqlc.narg(model)::text) AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
GROUP BY user_id ORDER BY request_count DESC, user_id NULLS LAST LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: AggregateUsageByAPIKey :many
SELECT api_key_id, count(*)::bigint AS request_count, count(*) FILTER (WHERE status = 'success')::bigint AS success_count, count(*) FILTER (WHERE status <> 'success')::bigint AS error_count, coalesce(sum(total_tokens), 0)::bigint AS total_tokens, coalesce(sum(total_cost), 0)::numeric(20, 6)::text AS total_cost, coalesce(sum(duration_ms), 0)::bigint AS duration_ms, count(*) OVER()::bigint AS aggregate_total
FROM usage_logs
WHERE user_id = sqlc.arg(owner_user_id) AND created_at >= sqlc.arg(start_time)::timestamptz AND created_at < sqlc.arg(end_time)::timestamptz
  AND (sqlc.narg(user_id)::bigint IS NULL OR user_id = sqlc.narg(user_id)::bigint) AND (sqlc.narg(api_key_id)::bigint IS NULL OR api_key_id = sqlc.narg(api_key_id)::bigint) AND (sqlc.narg(channel_id)::bigint IS NULL OR channel_id = sqlc.narg(channel_id)::bigint) AND (sqlc.narg(model)::text IS NULL OR model = sqlc.narg(model)::text) AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
GROUP BY api_key_id ORDER BY request_count DESC, api_key_id NULLS LAST LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: AggregateUsageByModel :many
SELECT model, count(*)::bigint AS request_count, count(*) FILTER (WHERE status = 'success')::bigint AS success_count, count(*) FILTER (WHERE status <> 'success')::bigint AS error_count, coalesce(sum(total_tokens), 0)::bigint AS total_tokens, coalesce(sum(total_cost), 0)::numeric(20, 6)::text AS total_cost, coalesce(sum(duration_ms), 0)::bigint AS duration_ms, count(*) OVER()::bigint AS aggregate_total
FROM usage_logs
WHERE user_id = sqlc.arg(owner_user_id) AND created_at >= sqlc.arg(start_time)::timestamptz AND created_at < sqlc.arg(end_time)::timestamptz
  AND (sqlc.narg(user_id)::bigint IS NULL OR user_id = sqlc.narg(user_id)::bigint) AND (sqlc.narg(api_key_id)::bigint IS NULL OR api_key_id = sqlc.narg(api_key_id)::bigint) AND (sqlc.narg(channel_id)::bigint IS NULL OR channel_id = sqlc.narg(channel_id)::bigint) AND (sqlc.narg(model)::text IS NULL OR model = sqlc.narg(model)::text) AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
GROUP BY model ORDER BY request_count DESC, model LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: AggregateUsageByChannel :many
SELECT channel_id, count(*)::bigint AS request_count, count(*) FILTER (WHERE status = 'success')::bigint AS success_count, count(*) FILTER (WHERE status <> 'success')::bigint AS error_count, coalesce(sum(total_tokens), 0)::bigint AS total_tokens, coalesce(sum(total_cost), 0)::numeric(20, 6)::text AS total_cost, coalesce(sum(duration_ms), 0)::bigint AS duration_ms, count(*) OVER()::bigint AS aggregate_total
FROM usage_logs
WHERE user_id = sqlc.arg(owner_user_id) AND created_at >= sqlc.arg(start_time)::timestamptz AND created_at < sqlc.arg(end_time)::timestamptz
  AND (sqlc.narg(user_id)::bigint IS NULL OR user_id = sqlc.narg(user_id)::bigint) AND (sqlc.narg(api_key_id)::bigint IS NULL OR api_key_id = sqlc.narg(api_key_id)::bigint) AND (sqlc.narg(channel_id)::bigint IS NULL OR channel_id = sqlc.narg(channel_id)::bigint) AND (sqlc.narg(model)::text IS NULL OR model = sqlc.narg(model)::text) AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
GROUP BY channel_id ORDER BY request_count DESC, channel_id NULLS LAST LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);
