CREATE TABLE IF NOT EXISTS membership_invite_codes (
    id SERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    membership_level INT NOT NULL,
    duration_days INT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    used_count INT NOT NULL DEFAULT 0,
    max_uses INT NOT NULL DEFAULT 50,
    note TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (membership_level >= 1 AND membership_level <= 3),
    CHECK (duration_days > 0),
    CHECK (max_uses > 0)
);

CREATE INDEX IF NOT EXISTS idx_membership_invite_codes_active
ON membership_invite_codes(is_active);
