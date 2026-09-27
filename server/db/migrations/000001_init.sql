-- Baseline schema for a fresh deployment, squashed from the former incremental
-- migrations 000001..000010. The project has not shipped, so no upgrade path
-- from the pre-squash version table is needed.

CREATE TABLE channels (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key_ciphertext TEXT NOT NULL,
    auth_type TEXT NOT NULL DEFAULT 'bearer',
    status INTEGER NOT NULL DEFAULT 1,
    weight INTEGER NOT NULL DEFAULT 100,
    priority INTEGER NOT NULL DEFAULT 0,
    balance NUMERIC(20, 6),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE channel_models (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    model_name TEXT NOT NULL,
    upstream_model TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_id, model_name)
);

CREATE TABLE model_pricing (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    model_name TEXT NOT NULL,
    input_price_per_1m NUMERIC(20, 8) NOT NULL,
    output_price_per_1m NUMERIC(20, 8) NOT NULL,
    cached_input_price_per_1m NUMERIC(20, 8),
    currency TEXT NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_id, model_name)
);

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    nickname TEXT NOT NULL DEFAULT '',
    user_group TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL DEFAULT 'active',
    password_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('active', 'suspended'))
);

CREATE TABLE user_balances (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    available_balance NUMERIC(20, 6) NOT NULL DEFAULT 0,
    frozen_balance NUMERIC(20, 6) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE balance_transactions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tx_type TEXT NOT NULL,
    amount NUMERIC(20, 6) NOT NULL,
    balance_after NUMERIC(20, 6) NOT NULL,
    related_order_id TEXT,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Recharge idempotency is scoped per user: the same related_order_id may be
-- reused by different users, but must be unique within a user.
CREATE UNIQUE INDEX balance_transactions_user_order_idx
    ON balance_transactions (user_id, related_order_id)
    WHERE related_order_id IS NOT NULL AND related_order_id <> '';

CREATE TABLE client_api_keys (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_name TEXT NOT NULL DEFAULT 'default',
    prefix TEXT NOT NULL DEFAULT 'sk-',
    key_hash TEXT NOT NULL UNIQUE,
    permissions JSONB NOT NULL DEFAULT '{"models":["*"]}'::jsonb,
    rate_limit_overrides JSONB,
    expires_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Composite target for quota_reservations so a reservation cannot pair a
    -- key with a different user.
    CONSTRAINT client_api_keys_id_user_id_unique UNIQUE (id, user_id)
);

CREATE TABLE rate_limit_rules (
    id BIGSERIAL PRIMARY KEY,
    rule_name TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_value TEXT NOT NULL DEFAULT '*',
    metric TEXT NOT NULL,
    limit_value BIGINT NOT NULL,
    window_seconds INTEGER NOT NULL,
    action TEXT NOT NULL DEFAULT 'reject',
    priority INTEGER NOT NULL DEFAULT 100,
    enabled BOOLEAN NOT NULL DEFAULT true,
    extras JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (target_type IN ('global', 'user', 'api_key', 'model', 'channel')),
    CHECK (metric IN ('rpm', 'tpm', 'rpd', 'concurrency')),
    -- Queueing synchronous proxy requests is unsupported, so 'reject' is the
    -- only valid action.
    CHECK (action = 'reject')
);

CREATE INDEX rate_limit_rules_enabled_idx ON rate_limit_rules (enabled, priority, id);

CREATE TABLE usage_logs (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES client_api_keys(id) ON DELETE SET NULL,
    channel_id BIGINT REFERENCES channels(id) ON DELETE SET NULL,
    model TEXT NOT NULL,
    upstream_model TEXT NOT NULL DEFAULT '',
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    unit_price_input_per_1m NUMERIC(20, 8) NOT NULL DEFAULT 0,
    unit_price_output_per_1m NUMERIC(20, 8) NOT NULL DEFAULT 0,
    total_cost NUMERIC(20, 6) NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    ttft_ms BIGINT,
    status TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    client_ip TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX usage_logs_created_at_idx ON usage_logs (created_at DESC, id DESC);
CREATE INDEX usage_logs_user_created_at_idx ON usage_logs (user_id, created_at DESC);
CREATE INDEX usage_logs_channel_created_at_idx ON usage_logs (channel_id, created_at DESC);
CREATE INDEX usage_logs_model_created_at_idx ON usage_logs (model, created_at DESC);
-- These indexes match the API Key, model, and channel aggregation windows.
CREATE INDEX usage_logs_api_key_created_at_idx ON usage_logs (api_key_id, created_at DESC);
CREATE INDEX usage_logs_api_key_model_created_at_idx ON usage_logs (api_key_id, model, created_at DESC);
CREATE INDEX usage_logs_api_key_channel_created_at_idx ON usage_logs (api_key_id, channel_id, created_at DESC);

-- Per-channel circuit breaker state. A missing row means "closed".
CREATE TABLE channel_health (
    channel_id BIGINT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'closed' CHECK (state IN ('closed', 'open', 'half-open')),
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    success_count BIGINT NOT NULL DEFAULT 0,
    failure_count BIGINT NOT NULL DEFAULT 0,
    opened_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE quota_policies (
    id BIGSERIAL PRIMARY KEY,
    policy_name TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES client_api_keys(id) ON DELETE CASCADE,
    period_type TEXT NOT NULL,
    token_limit BIGINT,
    cost_limit NUMERIC(20, 6),
    enabled BOOLEAN NOT NULL DEFAULT true,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (scope_type IN ('user', 'api_key')),
    CHECK (period_type IN ('day', 'month')),
    CHECK ((scope_type = 'user' AND user_id IS NOT NULL AND api_key_id IS NULL) OR
           (scope_type = 'api_key' AND user_id IS NULL AND api_key_id IS NOT NULL)),
    CHECK (token_limit IS NULL OR token_limit > 0),
    CHECK (cost_limit IS NULL OR cost_limit > 0),
    CHECK (token_limit IS NOT NULL OR cost_limit IS NOT NULL)
);

CREATE UNIQUE INDEX quota_policies_user_period_uidx
    ON quota_policies (user_id, period_type)
    WHERE scope_type = 'user' AND deleted_at IS NULL;
CREATE UNIQUE INDEX quota_policies_key_period_uidx
    ON quota_policies (api_key_id, period_type)
    WHERE scope_type = 'api_key' AND deleted_at IS NULL;

CREATE TABLE quota_buckets (
    policy_id BIGINT NOT NULL REFERENCES quota_policies(id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    used_tokens BIGINT NOT NULL DEFAULT 0,
    reserved_tokens BIGINT NOT NULL DEFAULT 0,
    used_cost NUMERIC(20, 6) NOT NULL DEFAULT 0,
    reserved_cost NUMERIC(20, 6) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (policy_id, period_start),
    CHECK (period_end > period_start),
    CHECK (used_tokens >= 0 AND reserved_tokens >= 0),
    CHECK (used_cost >= 0 AND reserved_cost >= 0)
);

CREATE TABLE quota_reservations (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id BIGINT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    estimated_tokens BIGINT NOT NULL,
    estimated_cost NUMERIC(20, 6) NOT NULL,
    actual_tokens BIGINT,
    actual_cost NUMERIC(20, 6),
    status TEXT NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (estimated_tokens >= 0 AND estimated_cost >= 0),
    CHECK (actual_tokens IS NULL OR actual_tokens >= 0),
    CHECK (actual_cost IS NULL OR actual_cost >= 0),
    CHECK (status IN ('pending', 'settled', 'released', 'expired')),
    CONSTRAINT quota_reservations_key_user_fkey
        FOREIGN KEY (api_key_id, user_id) REFERENCES client_api_keys(id, user_id) ON DELETE RESTRICT
);

CREATE INDEX quota_reservations_pending_expiry_idx
    ON quota_reservations (expires_at, id) WHERE status = 'pending';
CREATE INDEX quota_reservations_scope_idx
    ON quota_reservations (user_id, api_key_id, status, expires_at);

CREATE TABLE quota_reservation_items (
    reservation_id BIGINT NOT NULL REFERENCES quota_reservations(id) ON DELETE CASCADE,
    policy_id BIGINT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    reserved_tokens BIGINT NOT NULL,
    reserved_cost NUMERIC(20, 6) NOT NULL,
    actual_tokens BIGINT,
    actual_cost NUMERIC(20, 6),
    PRIMARY KEY (reservation_id, policy_id),
    FOREIGN KEY (policy_id, period_start) REFERENCES quota_buckets(policy_id, period_start) ON DELETE CASCADE,
    CHECK (reserved_tokens >= 0 AND reserved_cost >= 0)
);

CREATE TABLE rate_limit_counters (
    rule_id BIGINT NOT NULL REFERENCES rate_limit_rules(id) ON DELETE CASCADE,
    bucket_start TIMESTAMPTZ NOT NULL,
    current_count BIGINT NOT NULL DEFAULT 0,
    previous_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, bucket_start),
    CHECK (current_count >= 0 AND previous_count >= 0)
);

CREATE TABLE rate_limit_reservations (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT NOT NULL REFERENCES client_api_keys(id) ON DELETE CASCADE,
    model TEXT NOT NULL DEFAULT '',
    channel_id BIGINT REFERENCES channels(id) ON DELETE CASCADE,
    estimated_tokens BIGINT NOT NULL DEFAULT 0,
    bucket_starts JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at TIMESTAMPTZ,
    CHECK (estimated_tokens >= 0),
    CHECK (status IN ('pending', 'released', 'settled', 'expired'))
);

CREATE INDEX rate_limit_reservations_pending_expiry_idx ON rate_limit_reservations (expires_at, id) WHERE status = 'pending';

CREATE TABLE channel_health_buckets (
    channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    bucket_start TIMESTAMPTZ NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0,
    errors BIGINT NOT NULL DEFAULT 0,
    timeouts BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, bucket_start),
    CHECK (requests >= 0 AND errors >= 0 AND timeouts >= 0)
);

CREATE TABLE channel_breaker_configs (
    channel_id BIGINT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    window_seconds INTEGER NOT NULL,
    minimum_samples INTEGER NOT NULL,
    error_rate_percent INTEGER NOT NULL,
    timeout_rate_percent INTEGER NOT NULL,
    cooldown_seconds INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (window_seconds > 0 AND minimum_samples > 0 AND error_rate_percent BETWEEN 1 AND 100 AND timeout_rate_percent BETWEEN 1 AND 100 AND cooldown_seconds > 0)
);

CREATE TABLE channel_breaker_probes (
    channel_id BIGINT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    lease_id UUID NOT NULL,
    leased_until TIMESTAMPTZ NOT NULL
);
