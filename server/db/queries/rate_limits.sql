-- name: ListRateLimitRules :many
SELECT id, rule_name, target_type, target_value, metric, limit_value, action, priority, enabled, extras
FROM rate_limit_rules
WHERE owner_user_id = sqlc.arg(owner_user_id)
  AND (sqlc.narg(enabled)::boolean IS NULL OR enabled = sqlc.narg(enabled)::boolean)
ORDER BY priority, id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountRateLimitRules :one
SELECT count(*)::int
FROM rate_limit_rules
WHERE owner_user_id = sqlc.arg(owner_user_id)
  AND (sqlc.narg(enabled)::boolean IS NULL OR enabled = sqlc.narg(enabled)::boolean);

-- name: GetRateLimitRule :one
SELECT id, rule_name, target_type, target_value, metric, limit_value, action, priority, enabled, extras
FROM rate_limit_rules
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: CreateRateLimitRule :one
INSERT INTO rate_limit_rules (owner_user_id, rule_name, target_type, target_value, metric, limit_value, action, priority, enabled, extras)
VALUES (
    sqlc.arg(owner_user_id),
    sqlc.arg(rule_name),
    sqlc.arg(target_type),
    sqlc.arg(target_value),
    sqlc.arg(metric),
    sqlc.arg(limit_value),
    sqlc.arg(action),
    sqlc.arg(priority),
    sqlc.arg(enabled),
    sqlc.arg(extras)
)
RETURNING id;

-- name: UpdateRateLimitRuleEnabled :execrows
UPDATE rate_limit_rules
SET enabled = sqlc.arg(enabled), updated_at = now()
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: DeleteRateLimitRule :execrows
DELETE FROM rate_limit_rules WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: RateLimitAPIKeyOwnedByUser :one
SELECT EXISTS (SELECT 1 FROM client_api_keys WHERE id = sqlc.arg(id)::bigint AND user_id = sqlc.arg(owner_user_id)::bigint);

-- name: RateLimitChannelOwnedByUser :one
SELECT EXISTS (SELECT 1 FROM channels WHERE id = sqlc.arg(id)::bigint AND owner_user_id = sqlc.arg(owner_user_id)::bigint);

-- name: RateLimitModelOwnedByUser :one
SELECT EXISTS (
    SELECT 1 FROM channel_models cm
    JOIN channels c ON c.id = cm.channel_id
    WHERE cm.model_name = sqlc.arg(model_name)::text AND c.owner_user_id = sqlc.arg(owner_user_id)::bigint
);

-- name: InsertRateLimitReservation :one
INSERT INTO rate_limit_reservations (request_id, user_id, api_key_id, model, channel_id, estimated_tokens, expires_at)
VALUES (sqlc.arg(request_id), sqlc.arg(user_id), sqlc.arg(api_key_id), sqlc.arg(model), sqlc.narg(channel_id), sqlc.arg(estimated_tokens), sqlc.arg(expires_at))
RETURNING id;

-- name: FinalizeRateLimitReservation :execrows
UPDATE rate_limit_reservations SET status = 'settled' WHERE id = $1 AND status = 'pending';

-- name: ReleaseRateLimitReservation :execrows
UPDATE rate_limit_reservations SET status = 'released', released_at = now() WHERE id = $1 AND status = 'pending';

-- name: ReapRateLimitReservations :execrows
WITH expired AS (
    SELECT id FROM rate_limit_reservations
    WHERE status = 'pending' AND expires_at <= now()
    ORDER BY id LIMIT sqlc.arg(batch_limit)::integer
)
UPDATE rate_limit_reservations SET status = 'expired' WHERE id IN (SELECT id FROM expired);

-- name: CountActiveRateLimitReservations :one
SELECT count(*) FROM rate_limit_reservations
WHERE status = 'pending' AND expires_at > now()
  AND user_id = sqlc.arg(user_id)::bigint
  AND (sqlc.arg(api_key_id)::bigint = 0 OR api_key_id = sqlc.arg(api_key_id)::bigint)
  AND (sqlc.arg(model)::text = '' OR model = sqlc.arg(model)::text)
  AND (sqlc.arg(channel_id)::bigint = 0 OR channel_id = sqlc.arg(channel_id)::bigint);
