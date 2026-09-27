-- name: ListUsers :many
SELECT
    u.id,
    u.nickname,
    u.user_group,
    u.status,
    COALESCE(b.available_balance::text, '0.000000') AS available_balance,
    COALESCE(b.frozen_balance::text, '0.000000') AS frozen_balance
FROM users u
LEFT JOIN user_balances b ON b.user_id = u.id
ORDER BY u.id
LIMIT $1 OFFSET $2;

-- name: CountUsers :one
SELECT count(*)::int FROM users;

-- name: GetUser :one
SELECT id, nickname, user_group, status
FROM users
WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (nickname, user_group, status)
VALUES (sqlc.arg(nickname), sqlc.arg(user_group), sqlc.arg(status))
RETURNING id;

-- name: UpdateUser :execrows
UPDATE users
SET nickname = sqlc.arg(nickname), user_group = sqlc.arg(user_group), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: UpdateUserStatus :execrows
UPDATE users
SET status = sqlc.arg(status), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- name: CreateUserBalance :exec
INSERT INTO user_balances (user_id, available_balance, frozen_balance)
VALUES ($1, 0, 0)
ON CONFLICT (user_id) DO NOTHING;

-- name: LockUserBalance :one
SELECT user_id FROM user_balances WHERE user_id = $1 FOR UPDATE;

-- name: UpdateUserBalance :execrows
UPDATE user_balances
SET available_balance = NULLIF(sqlc.arg(available_balance), '')::numeric, updated_at = now()
WHERE user_id = sqlc.arg(user_id);

-- name: GetUserBalanceText :one
SELECT
    COALESCE(available_balance::text, '0.000000') AS available_balance,
    COALESCE(frozen_balance::text, '0.000000') AS frozen_balance
FROM user_balances
WHERE user_id = $1;

-- name: CreateBalanceTransaction :one
INSERT INTO balance_transactions (user_id, tx_type, amount, balance_after, related_order_id, description)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(tx_type),
    NULLIF(sqlc.arg(amount), '')::numeric,
    NULLIF(sqlc.arg(balance_after), '')::numeric,
    NULLIF(sqlc.arg(related_order_id), ''),
    sqlc.arg(description)
)
RETURNING id;

-- name: GetBalanceTransactionByOrder :one
SELECT
    id,
    user_id,
    tx_type,
    amount::text AS amount,
    balance_after::text AS balance_after,
    related_order_id,
    description,
    created_at
FROM balance_transactions
WHERE user_id = $1 AND related_order_id = $2;

-- name: CountBalanceTransactions :one
SELECT count(*)::int FROM balance_transactions WHERE user_id = $1;

-- name: ListBalanceTransactions :many
SELECT
    id,
    user_id,
    tx_type,
    amount::text AS amount,
    balance_after::text AS balance_after,
    related_order_id,
    description,
    created_at
FROM balance_transactions
WHERE user_id = $1
ORDER BY id DESC
LIMIT $2 OFFSET $3;

-- name: ListUserKeys :many
SELECT id, user_id, key_name, prefix, is_active, last_used_at, expires_at
FROM client_api_keys
WHERE user_id = $1
ORDER BY id
LIMIT $2 OFFSET $3;

-- name: CountUserKeys :one
SELECT count(*)::int FROM client_api_keys WHERE user_id = $1;

-- name: ListKeys :many
SELECT id, user_id, key_name, prefix, is_active, last_used_at, expires_at
FROM client_api_keys
ORDER BY id
LIMIT $1 OFFSET $2;

-- name: CountKeys :one
SELECT count(*)::int FROM client_api_keys;

-- name: GetKey :one
SELECT id, user_id, key_name, prefix, is_active, last_used_at, expires_at
FROM client_api_keys
WHERE id = $1 AND user_id = $2;

-- name: CreateKey :one
INSERT INTO client_api_keys (user_id, key_name, prefix, key_hash, permissions, rate_limit_overrides, expires_at, is_active)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(key_name),
    sqlc.arg(prefix),
    sqlc.arg(key_hash),
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

-- name: UpdateKeySecret :execrows
UPDATE client_api_keys
SET key_hash = sqlc.arg(key_hash), prefix = sqlc.arg(prefix), updated_at = now()
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
    k.rate_limit_overrides,
    u.status AS user_status,
    COALESCE(b.available_balance::text, '0.000000') AS available_balance,
    COALESCE(b.frozen_balance::text, '0.000000') AS frozen_balance
FROM client_api_keys k
JOIN users u ON u.id = k.user_id
LEFT JOIN user_balances b ON b.user_id = k.user_id
WHERE k.key_hash = $1;

-- name: UpdateKeyLastUsed :execrows
UPDATE client_api_keys
SET last_used_at = now(), updated_at = now()
WHERE id = $1;

-- name: GetUserCredentialsByUsername :one
SELECT id, username, nickname, password_hash
FROM users
WHERE username = $1;

-- name: CreateUserWithCredentials :one
INSERT INTO users (username, nickname, password_hash)
VALUES (sqlc.arg(username), sqlc.arg(nickname), sqlc.arg(password_hash))
RETURNING id;

-- name: GetAccountByID :one
SELECT id, username, nickname
FROM users
WHERE id = $1;

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
