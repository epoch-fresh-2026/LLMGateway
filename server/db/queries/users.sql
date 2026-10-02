-- name: GetUser :one
SELECT id, username, nickname
FROM users
WHERE id = $1;

-- name: GetUserCredentialsByUsername :one
SELECT id, username, nickname, password_hash
FROM users
WHERE username = $1;

-- name: GetUserCredentialsByID :one
SELECT id, username, nickname, password_hash
FROM users
WHERE id = $1;

-- name: CreateUserWithCredentials :one
INSERT INTO users (username, nickname, password_hash)
VALUES (sqlc.arg(username), sqlc.arg(nickname), sqlc.arg(password_hash))
RETURNING id;

-- name: GetAccountByID :one
SELECT id, username, nickname
FROM users
WHERE id = $1;

-- name: UpdateUserProfile :execrows
UPDATE users
SET nickname = sqlc.arg(nickname), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: UpdateUserPassword :execrows
UPDATE users
SET password_hash = sqlc.arg(password_hash), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at)
VALUES (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(expires_at))
RETURNING id;

-- GetSessionByTokenHash only returns live sessions; an expired token must not
-- authenticate even if its row has not been reaped yet.
-- name: GetSessionByTokenHash :one
SELECT id, token_hash, user_id, expires_at
FROM sessions
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteSessionByTokenHash :execrows
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE id IN (
    SELECT id FROM sessions WHERE expires_at <= now() ORDER BY id LIMIT sqlc.arg(max_rows)
);

-- name: ListUserKeys :many
SELECT id, user_id, key_name, prefix, key_suffix, is_active, created_at, last_used_at, expires_at
FROM client_api_keys
WHERE user_id = $1
ORDER BY id
LIMIT $2 OFFSET $3;

-- name: CountUserKeys :one
SELECT count(*)::int FROM client_api_keys WHERE user_id = $1;

-- name: GetKey :one
SELECT id, user_id, key_name, prefix, key_suffix, is_active, created_at, last_used_at, expires_at
FROM client_api_keys
WHERE id = $1 AND user_id = $2;

-- name: CreateKey :one
INSERT INTO client_api_keys (user_id, key_name, prefix, key_hash, key_suffix, permissions, rate_limit_overrides, expires_at, is_active)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(key_name),
    sqlc.arg(prefix),
    sqlc.arg(key_hash),
    sqlc.arg(key_suffix),
    sqlc.arg(permissions),
    sqlc.narg(rate_limit_overrides),
    NULLIF(sqlc.arg(expires_at), '')::timestamptz,
    sqlc.arg(is_active)
)
RETURNING id;

-- name: UpdateKeyActive :execrows
UPDATE client_api_keys
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: DeleteKey :execrows
DELETE FROM client_api_keys WHERE id = $1 AND user_id = $2;

-- name: GetAuthContextByKeyHash :one
SELECT
    k.id AS key_id,
    k.user_id,
    k.key_name,
    k.is_active AS key_active,
    k.expires_at,
    k.permissions,
    k.rate_limit_overrides
FROM client_api_keys k
WHERE k.key_hash = $1;

-- name: UpdateKeyLastUsed :execrows
UPDATE client_api_keys
SET last_used_at = now(), updated_at = now()
WHERE id = $1;
