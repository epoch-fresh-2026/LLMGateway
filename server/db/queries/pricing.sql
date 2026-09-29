-- name: ListPricing :many
SELECT
    p.id,
    p.channel_id,
    c.name AS channel_name,
    p.upstream_model,
    p.input_price_per_1m::text AS input_price_per_1m,
    p.output_price_per_1m::text AS output_price_per_1m,
    COALESCE(p.cached_input_price_per_1m::text, ''::text) AS cached_input_price_per_1m,
    p.currency
FROM model_pricing p
JOIN channels c ON c.id = p.channel_id
WHERE c.owner_user_id = sqlc.arg(owner_user_id)
ORDER BY p.id;

-- name: GetPricing :one
SELECT
    p.id,
    p.channel_id,
    c.name AS channel_name,
    p.upstream_model,
    p.input_price_per_1m::text AS input_price_per_1m,
    p.output_price_per_1m::text AS output_price_per_1m,
    COALESCE(p.cached_input_price_per_1m::text, ''::text) AS cached_input_price_per_1m,
    p.currency
FROM model_pricing p
JOIN channels c ON c.id = p.channel_id
WHERE p.channel_id = $1 AND p.upstream_model = $2;

-- name: UpsertPricing :one
INSERT INTO model_pricing (channel_id, upstream_model, input_price_per_1m, output_price_per_1m, cached_input_price_per_1m, currency)
VALUES (
    sqlc.arg(channel_id),
    sqlc.arg(upstream_model),
    NULLIF(sqlc.arg(input_price_per_1m), '')::numeric,
    NULLIF(sqlc.arg(output_price_per_1m), '')::numeric,
    NULLIF(sqlc.arg(cached_input_price_per_1m), '')::numeric,
    sqlc.arg(currency)
)
ON CONFLICT (channel_id, upstream_model)
DO UPDATE SET
    input_price_per_1m = EXCLUDED.input_price_per_1m,
    output_price_per_1m = EXCLUDED.output_price_per_1m,
    cached_input_price_per_1m = EXCLUDED.cached_input_price_per_1m,
    currency = EXCLUDED.currency,
    updated_at = now()
RETURNING id;

-- name: DeletePricing :exec
DELETE FROM model_pricing WHERE channel_id = $1 AND upstream_model = $2;
