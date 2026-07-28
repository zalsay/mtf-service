CREATE TABLE IF NOT EXISTS open_api_v2_keys (
    id BIGSERIAL PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    server_name TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP WITH TIME ZONE,
    CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_open_api_v2_keys_server_user
    ON open_api_v2_keys(server_name, external_user_id);
