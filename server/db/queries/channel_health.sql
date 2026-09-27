-- name: GetChannelHealth :one
SELECT channel_id, state, consecutive_failures, success_count, failure_count, opened_at, updated_at
FROM channel_health
WHERE channel_id = $1;

-- name: EnsureChannelHealth :exec
INSERT INTO channel_health (channel_id)
VALUES ($1)
ON CONFLICT (channel_id) DO NOTHING;

-- name: GetChannelHealthForUpdate :one
SELECT channel_id, state, consecutive_failures, success_count, failure_count, opened_at, updated_at
FROM channel_health
WHERE channel_id = $1
FOR UPDATE;

-- name: UpdateChannelHealth :execrows
UPDATE channel_health
SET state = sqlc.arg(state),
    consecutive_failures = sqlc.arg(consecutive_failures),
    success_count = sqlc.arg(success_count),
    failure_count = sqlc.arg(failure_count),
    opened_at = sqlc.narg(opened_at),
    updated_at = now()
WHERE channel_id = sqlc.arg(channel_id);

-- name: DeleteChannelHealth :execrows
DELETE FROM channel_health WHERE channel_id = $1;

-- name: ListChannelHealth :many
SELECT h.channel_id, h.state, h.consecutive_failures, h.success_count, h.failure_count, h.opened_at, h.updated_at
FROM channel_health h
JOIN channels c ON c.id = h.channel_id
WHERE c.owner_user_id = sqlc.arg(owner_user_id)
ORDER BY h.channel_id;

-- name: UpsertChannelHealthBucket :exec
INSERT INTO channel_health_buckets (channel_id, bucket_start, requests, errors, timeouts)
VALUES (sqlc.arg(channel_id), sqlc.arg(bucket_start), sqlc.arg(requests), sqlc.arg(errors), sqlc.arg(timeouts))
ON CONFLICT (channel_id, bucket_start) DO UPDATE
SET requests = channel_health_buckets.requests + EXCLUDED.requests,
    errors = channel_health_buckets.errors + EXCLUDED.errors,
    timeouts = channel_health_buckets.timeouts + EXCLUDED.timeouts;

-- name: SumChannelHealthWindow :one
SELECT
    COALESCE(SUM(requests), 0)::bigint AS requests,
    COALESCE(SUM(errors), 0)::bigint AS errors,
    COALESCE(SUM(timeouts), 0)::bigint AS timeouts
FROM channel_health_buckets
WHERE channel_id = sqlc.arg(channel_id) AND bucket_start >= sqlc.arg(since);

-- name: DeleteStaleChannelHealthBuckets :execrows
DELETE FROM channel_health_buckets WHERE bucket_start < sqlc.arg(before);

-- name: GetChannelBreakerConfig :one
SELECT channel_id, window_seconds, minimum_samples, error_rate_percent, timeout_rate_percent, cooldown_seconds
FROM channel_breaker_configs
WHERE channel_id = $1;

-- name: ListChannelBreakerConfigs :many
SELECT b.channel_id, b.window_seconds, b.minimum_samples, b.error_rate_percent, b.timeout_rate_percent, b.cooldown_seconds
FROM channel_breaker_configs b
JOIN channels c ON c.id = b.channel_id
WHERE c.owner_user_id = sqlc.arg(owner_user_id)
ORDER BY b.channel_id;

-- name: UpsertChannelBreakerConfig :exec
INSERT INTO channel_breaker_configs (channel_id, window_seconds, minimum_samples, error_rate_percent, timeout_rate_percent, cooldown_seconds, updated_at)
VALUES (sqlc.arg(channel_id), sqlc.arg(window_seconds), sqlc.arg(minimum_samples), sqlc.arg(error_rate_percent), sqlc.arg(timeout_rate_percent), sqlc.arg(cooldown_seconds), now())
ON CONFLICT (channel_id) DO UPDATE
SET window_seconds = EXCLUDED.window_seconds,
    minimum_samples = EXCLUDED.minimum_samples,
    error_rate_percent = EXCLUDED.error_rate_percent,
    timeout_rate_percent = EXCLUDED.timeout_rate_percent,
    cooldown_seconds = EXCLUDED.cooldown_seconds,
    updated_at = now();

-- name: DeleteChannelBreakerConfig :execrows
DELETE FROM channel_breaker_configs WHERE channel_id = $1;
