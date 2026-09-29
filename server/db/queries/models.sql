-- name: ListChannelModels :many
SELECT cm.id, cm.model_name, cm.upstream_model, cm.enabled
FROM channel_models cm
JOIN channels c ON c.id = cm.channel_id
WHERE cm.channel_id = sqlc.arg(channel_id) AND c.owner_user_id = sqlc.arg(owner_user_id)
ORDER BY cm.id;

-- name: GetChannelModel :one
SELECT cm.id, cm.model_name, cm.upstream_model, cm.enabled
FROM channel_models cm
JOIN channels c ON c.id = cm.channel_id
WHERE cm.channel_id = sqlc.arg(channel_id)
  AND cm.model_name = sqlc.arg(model_name)
  AND c.owner_user_id = sqlc.arg(owner_user_id);

-- name: CreateChannelModel :one
INSERT INTO channel_models (channel_id, model_name, upstream_model, enabled)
VALUES (sqlc.arg(channel_id), sqlc.arg(model_name), sqlc.arg(upstream_model), sqlc.arg(enabled))
RETURNING id, model_name, upstream_model, enabled;

-- The upstream (real) model name is set by the upstream and cannot change;
-- only the public alias and enabled flag are updated.
-- name: UpdateChannelModel :one
UPDATE channel_models
SET model_name = COALESCE(NULLIF(sqlc.arg(model_name), ''), model_name),
    enabled = sqlc.arg(enabled),
    updated_at = now()
WHERE channel_id = sqlc.arg(channel_id) AND id = sqlc.arg(id)
RETURNING id, model_name, upstream_model, enabled;

-- name: GetChannelModelByID :one
SELECT cm.id, cm.model_name, cm.upstream_model, cm.enabled
FROM channel_models cm
WHERE cm.channel_id = sqlc.arg(channel_id) AND cm.id = sqlc.arg(id);

-- name: ChannelUpstreamExists :one
SELECT 1 FROM channel_models cm
JOIN channels c ON c.id = cm.channel_id
WHERE cm.channel_id = sqlc.arg(channel_id)
  AND cm.upstream_model = sqlc.arg(upstream_model)
  AND c.owner_user_id = sqlc.arg(owner_user_id)
LIMIT 1;

-- name: DeleteChannelModel :execrows
DELETE FROM channel_models WHERE channel_id = $1 AND id = $2;

-- name: ListRouteCandidates :many
SELECT
    c.id AS channel_id,
    c.name AS channel_name,
    cm.upstream_model,
    c.priority,
    c.weight,
    COALESCE(c.balance::text, '') AS balance
FROM channel_models cm
JOIN channels c ON c.id = cm.channel_id
LEFT JOIN channel_health h ON h.channel_id = c.id
LEFT JOIN channel_breaker_configs cbc ON cbc.channel_id = c.id
WHERE cm.model_name = sqlc.arg(model_name) AND cm.enabled = true AND c.status = 1
  AND c.owner_user_id = sqlc.arg(owner_user_id)
  -- Exclude open channels, but treat them as half-open (allowed) once the
  -- cooldown has elapsed; a missing health row means closed. The cooldown is
  -- the per-channel override when set, otherwise the global default.
  AND NOT (
      COALESCE(h.state, 'closed') = 'open'
      AND (
          h.opened_at IS NULL
          OR h.opened_at + (COALESCE(cbc.cooldown_seconds, sqlc.arg(default_cooldown_seconds)::int) * interval '1 second') > now()
      )
  )
ORDER BY c.priority DESC, c.weight DESC, c.id;

-- name: ListCatalogModels :many
SELECT
    cm.model_name,
    c.id AS channel_id,
    c.name AS channel_name,
    cm.upstream_model,
    cm.enabled
FROM channel_models cm
JOIN channels c ON c.id = cm.channel_id
WHERE c.owner_user_id = sqlc.arg(owner_user_id)
  AND (sqlc.arg(enabled_only)::boolean = false OR cm.enabled = true)
ORDER BY cm.model_name, c.priority DESC, c.weight DESC, c.id;
