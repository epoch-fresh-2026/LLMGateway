-- name: ListChannels :many
SELECT
    c.id,
    c.name,
    c.base_url,
    c.auth_type,
    c.status,
    c.weight,
    c.priority,
    COALESCE(c.balance::text, ''::text) AS balance,
    count(cm.id)::int AS model_count
FROM channels c
LEFT JOIN channel_models cm ON cm.channel_id = c.id
WHERE c.owner_user_id = sqlc.arg(owner_user_id)
GROUP BY c.id
ORDER BY c.id;

-- name: GetChannel :one
SELECT
    c.id,
    c.name,
    c.base_url,
    c.auth_type,
    c.status,
    c.weight,
    c.priority,
    COALESCE(c.balance::text, ''::text) AS balance,
    count(cm.id)::int AS model_count
FROM channels c
LEFT JOIN channel_models cm ON cm.channel_id = c.id
WHERE c.id = sqlc.arg(id) AND c.owner_user_id = sqlc.arg(owner_user_id)
GROUP BY c.id;

-- name: LockChannel :one
SELECT id FROM channels
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id)
FOR UPDATE;

-- name: GetChannelSecret :one
SELECT
    id,
    name,
    base_url,
    api_key_ciphertext,
    auth_type,
    status,
    weight,
    priority,
    COALESCE(balance::text, ''::text) AS balance
FROM channels
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: CreateChannel :one
INSERT INTO channels (owner_user_id, name, base_url, api_key_ciphertext, auth_type, status, weight, priority, balance)
VALUES (
    sqlc.arg(owner_user_id),
    sqlc.arg(name),
    sqlc.arg(base_url),
    sqlc.arg(api_key_ciphertext),
    sqlc.arg(auth_type),
    sqlc.arg(status),
    sqlc.arg(weight),
    sqlc.arg(priority),
    NULLIF(sqlc.arg(balance), '')::numeric
)
RETURNING id;

-- name: UpdateChannel :execrows
UPDATE channels
SET name = sqlc.arg(name),
    base_url = sqlc.arg(base_url),
    auth_type = sqlc.arg(auth_type),
    status = sqlc.arg(status),
    weight = sqlc.arg(weight),
    priority = sqlc.arg(priority),
    balance = NULLIF(sqlc.arg(balance), '')::numeric,
    api_key_ciphertext = COALESCE(NULLIF(sqlc.arg(api_key_ciphertext), ''), api_key_ciphertext),
    updated_at = now()
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: UpdateChannelStatus :execrows
UPDATE channels
SET status = sqlc.arg(status), updated_at = now()
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: UpdateChannelBalance :execrows
UPDATE channels
SET balance = NULLIF(sqlc.arg(balance), '')::numeric, updated_at = now()
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: DeleteChannel :execrows
DELETE FROM channels WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: GetChannelOwner :one
SELECT owner_user_id FROM channels WHERE id = sqlc.arg(id);
