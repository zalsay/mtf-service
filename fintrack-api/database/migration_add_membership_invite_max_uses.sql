ALTER TABLE membership_invite_codes
ADD COLUMN IF NOT EXISTS max_uses INT NOT NULL DEFAULT 50;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'membership_invite_codes_max_uses_check'
    ) THEN
        ALTER TABLE membership_invite_codes
        ADD CONSTRAINT membership_invite_codes_max_uses_check CHECK (max_uses > 0);
    END IF;
END $$;
