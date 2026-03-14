-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS markets (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    question TEXT NOT NULL,
    condition_id TEXT NOT NULL UNIQUE,
    slug TEXT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    token_up TEXT NOT NULL,
    token_down TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('planned','active','resolved','redeemed')),
    underlying TEXT NOT NULL CHECK (underlying IN ('BTC','ETH','XRP','SOL')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    status TEXT NULL,
    error BOOLEAN NOT NULL DEFAULT false,
    error_msg TEXT NULL,
    condition_id TEXT NOT NULL,
    original_size NUMERIC NOT NULL,
    matched_size NUMERIC NOT NULL DEFAULT 0,
    price NUMERIC NOT NULL,
    token_id TEXT NOT NULL,
    outcome VARCHAR(4) NOT NULL CHECK (outcome IN ('Up','Down')),
    associate_trades TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS stats (
    id BIGSERIAL PRIMARY KEY,
    condition_id TEXT NOT NULL,
    stat_type TEXT NOT NULL,
    stat JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes: markets
CREATE INDEX IF NOT EXISTS idx_markets_condition_id ON markets(condition_id);
CREATE INDEX IF NOT EXISTS idx_markets_event_id ON markets(event_id);
CREATE INDEX IF NOT EXISTS idx_markets_slug ON markets(slug);
CREATE INDEX IF NOT EXISTS idx_markets_status ON markets(status);
CREATE INDEX IF NOT EXISTS idx_markets_underlying ON markets(underlying);
CREATE INDEX IF NOT EXISTS idx_markets_status_underlying ON markets(status, underlying);
CREATE INDEX IF NOT EXISTS idx_markets_start_time ON markets(start_time);
CREATE INDEX IF NOT EXISTS idx_markets_end_time ON markets(end_time);
CREATE INDEX IF NOT EXISTS idx_markets_underlying_end_time ON markets(underlying, end_time DESC);

-- Indexes: orders
CREATE INDEX IF NOT EXISTS idx_orders_condition_id ON orders(condition_id);
CREATE INDEX IF NOT EXISTS idx_orders_token_id ON orders(token_id);
CREATE INDEX IF NOT EXISTS idx_orders_outcome ON orders(outcome);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_condition_created ON orders(condition_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_condition_status ON orders(condition_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_condition_token_created ON orders(condition_id, token_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_condition_matched ON orders(condition_id, matched_size)
WHERE matched_size > 0;

-- Indexes: stats
CREATE INDEX IF NOT EXISTS idx_stats_condition_id ON stats(condition_id);
CREATE INDEX IF NOT EXISTS idx_stats_type ON stats(stat_type);
CREATE INDEX IF NOT EXISTS idx_stats_created_at ON stats(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_stats_condition_type_created ON stats(condition_id, stat_type, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stats CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS markets CASCADE;
-- +goose StatementEnd
