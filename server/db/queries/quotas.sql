-- name: KeyBelongsToUser :one
SELECT EXISTS (
    SELECT 1 FROM client_api_keys
    WHERE id = sqlc.arg(key_id)::bigint AND user_id = sqlc.arg(owner_user_id)::bigint
);

-- name: ListQuotaPolicies :many
SELECT p.id, p.policy_name, p.scope_type, p.user_id, p.api_key_id, p.period_type,
       p.token_limit, p.cost_limit::text AS cost_limit, p.enabled
FROM quota_policies p
WHERE p.deleted_at IS NULL
  AND ((p.scope_type = 'user' AND p.user_id = sqlc.arg(owner_user_id)::bigint)
       OR (p.scope_type = 'api_key' AND p.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id)::bigint)))
  AND (sqlc.arg(scope_type)::text = '' OR p.scope_type = sqlc.arg(scope_type)::text)
  AND (sqlc.arg(scope_id)::bigint = 0 OR COALESCE(p.user_id, p.api_key_id) = sqlc.arg(scope_id)::bigint)
  AND (sqlc.narg(enabled)::boolean IS NULL OR p.enabled = sqlc.narg(enabled)::boolean)
ORDER BY p.id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: CountQuotaPolicies :one
SELECT count(*)
FROM quota_policies p
WHERE p.deleted_at IS NULL
  AND ((p.scope_type = 'user' AND p.user_id = sqlc.arg(owner_user_id)::bigint)
       OR (p.scope_type = 'api_key' AND p.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id)::bigint)))
  AND (sqlc.arg(scope_type)::text = '' OR p.scope_type = sqlc.arg(scope_type)::text)
  AND (sqlc.arg(scope_id)::bigint = 0 OR COALESCE(p.user_id, p.api_key_id) = sqlc.arg(scope_id)::bigint)
  AND (sqlc.narg(enabled)::boolean IS NULL OR p.enabled = sqlc.narg(enabled)::boolean);

-- name: ListQuotaUsage :many
SELECT p.id AS policy_id, p.policy_name, p.scope_type, p.user_id, p.api_key_id, p.period_type,
       b.period_start, b.period_end, p.token_limit, b.used_tokens, b.reserved_tokens,
       p.cost_limit::text AS cost_limit, b.used_cost::text AS used_cost,
       b.reserved_cost::text AS reserved_cost
FROM quota_policies p JOIN quota_buckets b ON b.policy_id = p.id
WHERE p.deleted_at IS NULL
  AND ((p.scope_type = 'user' AND p.user_id = sqlc.arg(owner_user_id)::bigint)
       OR (p.scope_type = 'api_key' AND p.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id)::bigint)))
  AND (sqlc.arg(scope_type)::text = '' OR p.scope_type = sqlc.arg(scope_type)::text)
  AND (sqlc.arg(scope_id)::bigint = 0 OR COALESCE(p.user_id, p.api_key_id) = sqlc.arg(scope_id)::bigint)
  AND b.period_start <= now() AND b.period_end > now()
ORDER BY p.id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: CountQuotaUsage :one
SELECT count(*)
FROM quota_policies p JOIN quota_buckets b ON b.policy_id = p.id
WHERE p.deleted_at IS NULL
  AND ((p.scope_type = 'user' AND p.user_id = sqlc.arg(owner_user_id)::bigint)
       OR (p.scope_type = 'api_key' AND p.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id)::bigint)))
  AND (sqlc.arg(scope_type)::text = '' OR p.scope_type = sqlc.arg(scope_type)::text)
  AND (sqlc.arg(scope_id)::bigint = 0 OR COALESCE(p.user_id, p.api_key_id) = sqlc.arg(scope_id)::bigint)
  AND b.period_start <= now() AND b.period_end > now();

-- name: GetQuotaPolicy :one
SELECT id, policy_name, scope_type, user_id, api_key_id, period_type,
       token_limit, COALESCE(cost_limit::text, '') AS cost_limit, enabled
FROM quota_policies
WHERE quota_policies.id = sqlc.arg(id) AND deleted_at IS NULL
  AND ((quota_policies.scope_type = 'user' AND quota_policies.user_id = sqlc.arg(owner_user_id))
       OR (quota_policies.scope_type = 'api_key' AND quota_policies.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id))));

-- name: CreateQuotaPolicy :one
INSERT INTO quota_policies (policy_name, scope_type, user_id, api_key_id, period_type, token_limit, cost_limit, enabled)
VALUES (sqlc.arg(policy_name), sqlc.arg(scope_type), sqlc.narg(user_id), sqlc.narg(api_key_id),
        sqlc.arg(period_type), sqlc.narg(token_limit), sqlc.narg(cost_limit), sqlc.arg(enabled))
RETURNING id;

-- name: UpdateQuotaPolicy :execrows
UPDATE quota_policies
SET policy_name = sqlc.arg(policy_name), token_limit = sqlc.narg(token_limit),
    cost_limit = sqlc.narg(cost_limit), enabled = sqlc.arg(enabled), updated_at = now()
WHERE quota_policies.id = sqlc.arg(id) AND deleted_at IS NULL
  AND ((quota_policies.scope_type = 'user' AND quota_policies.user_id = sqlc.arg(owner_user_id))
       OR (quota_policies.scope_type = 'api_key' AND quota_policies.api_key_id IN (
           SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id))));

-- name: DeleteQuotaPolicy :execrows
UPDATE quota_policies
SET enabled = false, deleted_at = now(), updated_at = now()
WHERE quota_policies.id = sqlc.arg(id) AND deleted_at IS NULL
  AND ((quota_policies.scope_type = 'user' AND quota_policies.user_id = sqlc.arg(owner_user_id))
       OR (quota_policies.scope_type = 'api_key' AND quota_policies.api_key_id IN (
            SELECT k.id FROM client_api_keys k WHERE k.user_id = sqlc.arg(owner_user_id))));

-- name: InsertQuotaReservation :one
INSERT INTO quota_reservations (request_id, user_id, api_key_id, model, estimated_tokens, estimated_cost, expires_at)
VALUES (sqlc.arg(request_id), sqlc.arg(user_id), sqlc.arg(api_key_id), sqlc.arg(model), sqlc.arg(estimated_tokens), sqlc.arg(estimated_cost)::text::numeric, sqlc.arg(expires_at))
RETURNING id;

-- name: UpsertQuotaBucket :exec
INSERT INTO quota_buckets (policy_id, period_start, period_end)
VALUES (sqlc.arg(policy_id), sqlc.arg(period_start), sqlc.arg(period_end))
ON CONFLICT DO NOTHING;

-- name: LockQuotaBucket :one
SELECT policy_id FROM quota_buckets
WHERE policy_id = sqlc.arg(policy_id) AND period_start = sqlc.arg(period_start)
FOR UPDATE;

-- name: ReserveQuotaBucket :execrows
UPDATE quota_buckets b
SET reserved_tokens = b.reserved_tokens + sqlc.arg(tokens)::bigint,
    reserved_cost = b.reserved_cost + sqlc.arg(cost)::text::numeric,
    updated_at = now()
FROM quota_policies p
WHERE b.policy_id = sqlc.arg(policy_id) AND b.period_start = sqlc.arg(period_start) AND p.id = b.policy_id
  AND p.enabled = true AND p.deleted_at IS NULL
  AND (p.token_limit IS NULL OR b.used_tokens + b.reserved_tokens + sqlc.arg(tokens)::bigint <= p.token_limit)
  AND (p.cost_limit IS NULL OR b.used_cost + b.reserved_cost + sqlc.arg(cost)::text::numeric <= p.cost_limit);

-- name: InsertQuotaReservationItem :exec
INSERT INTO quota_reservation_items (reservation_id, policy_id, period_start, reserved_tokens, reserved_cost)
VALUES (sqlc.arg(reservation_id), sqlc.arg(policy_id), sqlc.arg(period_start), sqlc.arg(tokens), sqlc.arg(cost)::text::numeric);

-- name: ApplicableQuotaPolicies :many
SELECT id, policy_name, scope_type, COALESCE(user_id, api_key_id)::bigint AS scope_id,
       period_type, token_limit, COALESCE(cost_limit::text, '')::text AS cost_limit, enabled
FROM quota_policies
WHERE deleted_at IS NULL AND enabled = true
  AND ((scope_type = 'user' AND user_id = sqlc.arg(user_id)::bigint)
       OR (scope_type = 'api_key' AND api_key_id = sqlc.arg(key_id)::bigint))
ORDER BY id FOR SHARE;

-- name: LockQuotaReservationStatus :one
SELECT status FROM quota_reservations WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ReleaseQuotaBucket :execrows
UPDATE quota_buckets
SET reserved_tokens = reserved_tokens - sqlc.arg(tokens)::bigint,
    reserved_cost = reserved_cost - sqlc.arg(cost)::text::numeric, updated_at = now()
WHERE policy_id = sqlc.arg(policy_id) AND period_start = sqlc.arg(period_start)
  AND reserved_tokens >= sqlc.arg(tokens)::bigint AND reserved_cost >= sqlc.arg(cost)::text::numeric;

-- name: ReleaseQuotaReservation :exec
UPDATE quota_reservations
SET status = sqlc.arg(status), released_at = sqlc.arg(released_at), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: LockExpiredQuotaReservations :many
SELECT id FROM quota_reservations
WHERE status = 'pending' AND expires_at <= sqlc.arg(expires_at)
  AND (sqlc.arg(user_id)::bigint = 0 OR user_id = sqlc.arg(user_id)::bigint OR api_key_id = sqlc.arg(key_id)::bigint)
ORDER BY expires_at, id FOR UPDATE SKIP LOCKED LIMIT sqlc.arg(row_limit)::bigint;

-- name: LockQuotaReservationItems :many
SELECT policy_id, period_start, reserved_tokens, reserved_cost::text AS reserved_cost
FROM quota_reservation_items WHERE reservation_id = sqlc.arg(reservation_id)
ORDER BY policy_id FOR UPDATE;

-- name: LockQuotaReservationIdentity :one
SELECT status, request_id, user_id, api_key_id FROM quota_reservations
WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: SettleQuotaBucket :execrows
UPDATE quota_buckets
SET reserved_tokens = reserved_tokens - sqlc.arg(reserved_tokens)::bigint,
    reserved_cost = reserved_cost - sqlc.arg(reserved_cost)::text::numeric,
    used_tokens = used_tokens + sqlc.arg(actual_tokens)::bigint,
    used_cost = used_cost + sqlc.arg(actual_cost)::text::numeric, updated_at = now()
WHERE policy_id = sqlc.arg(policy_id) AND period_start = sqlc.arg(period_start)
  AND reserved_tokens >= sqlc.arg(reserved_tokens)::bigint AND reserved_cost >= sqlc.arg(reserved_cost)::text::numeric
  AND sqlc.arg(actual_tokens)::bigint <= sqlc.arg(reserved_tokens)::bigint
  AND sqlc.arg(actual_cost)::text::numeric <= sqlc.arg(reserved_cost)::text::numeric;

-- name: SettleQuotaReservationItem :exec
UPDATE quota_reservation_items
SET actual_tokens = sqlc.arg(actual_tokens), actual_cost = sqlc.arg(actual_cost)::text::numeric
WHERE reservation_id = sqlc.arg(reservation_id) AND policy_id = sqlc.arg(policy_id);

-- name: SettleQuotaReservation :exec
UPDATE quota_reservations
SET status = 'settled', actual_tokens = sqlc.arg(actual_tokens), actual_cost = sqlc.arg(actual_cost)::text::numeric,
    settled_at = sqlc.arg(settled_at), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: LockQuotaPoliciesForCleanup :exec
SELECT id FROM quota_policies
WHERE deleted_at IS NULL
  AND ((sqlc.arg(user_id)::bigint > 0 AND scope_type = 'user' AND user_id = sqlc.arg(user_id)::bigint)
       OR (sqlc.arg(key_id)::bigint > 0 AND scope_type = 'api_key' AND api_key_id = sqlc.arg(key_id)::bigint)
       OR (sqlc.arg(user_id)::bigint > 0 AND scope_type = 'api_key' AND api_key_id IN (
           SELECT id FROM client_api_keys WHERE user_id = sqlc.arg(user_id)::bigint)))
ORDER BY id FOR UPDATE;

-- name: LockQuotaReservationsForCleanup :many
SELECT id FROM quota_reservations
WHERE (sqlc.arg(user_id)::bigint = 0 OR user_id = sqlc.arg(user_id)::bigint)
  AND (sqlc.arg(key_id)::bigint = 0 OR api_key_id = sqlc.arg(key_id)::bigint)
ORDER BY id FOR UPDATE;

-- name: DeleteKeyQuotaReservations :exec
DELETE FROM quota_reservations
WHERE api_key_id = sqlc.arg(key_id)::bigint AND (sqlc.arg(user_id)::bigint = 0 OR user_id = sqlc.arg(user_id)::bigint);

-- name: DeleteUserQuotaReservations :exec
DELETE FROM quota_reservations WHERE user_id = sqlc.arg(user_id)::bigint;
