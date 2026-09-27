-- name: ListRateLimitRules :many
SELECT id, rule_name, target_type, target_value, metric, limit_value, window_seconds, action, priority, enabled, extras
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
SELECT id, rule_name, target_type, target_value, metric, limit_value, window_seconds, action, priority, enabled, extras
FROM rate_limit_rules
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: CreateRateLimitRule :one
INSERT INTO rate_limit_rules (owner_user_id, rule_name, target_type, target_value, metric, limit_value, window_seconds, action, priority, enabled, extras)
VALUES (
    sqlc.arg(owner_user_id),
    sqlc.arg(rule_name),
    sqlc.arg(target_type),
    sqlc.arg(target_value),
    sqlc.arg(metric),
    sqlc.arg(limit_value),
    sqlc.arg(window_seconds),
    sqlc.arg(action),
    sqlc.arg(priority),
    sqlc.arg(enabled),
    sqlc.arg(extras)
)
RETURNING id;

-- name: UpdateRateLimitRule :execrows
UPDATE rate_limit_rules
SET rule_name = sqlc.arg(rule_name),
    target_type = sqlc.arg(target_type),
    target_value = sqlc.arg(target_value),
    metric = sqlc.arg(metric),
    limit_value = sqlc.arg(limit_value),
    window_seconds = sqlc.arg(window_seconds),
    action = sqlc.arg(action),
    priority = sqlc.arg(priority),
    enabled = sqlc.arg(enabled),
    extras = sqlc.arg(extras),
    updated_at = now()
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: UpdateRateLimitRuleEnabled :execrows
UPDATE rate_limit_rules
SET enabled = sqlc.arg(enabled), updated_at = now()
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: DeleteRateLimitRule :execrows
DELETE FROM rate_limit_rules WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);
