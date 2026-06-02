package services

import (
	"fmt"

	"fintrack-api/database"
)

const ensureMembershipInviteMaxUsesSQL = `
ALTER TABLE membership_invite_codes
ADD COLUMN IF NOT EXISTS max_uses INT NOT NULL DEFAULT 50;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'membership_invite_codes_max_uses_check'
          AND conrelid = 'membership_invite_codes'::regclass
    ) THEN
        ALTER TABLE membership_invite_codes
        ADD CONSTRAINT membership_invite_codes_max_uses_check CHECK (max_uses > 0);
    END IF;
END $$;
`

func ensureMembershipInviteMaxUsesColumn(db *database.DB) error {
	if db == nil || db.Conn == nil {
		return fmt.Errorf("database is not configured")
	}
	if _, err := db.Conn.Exec(ensureMembershipInviteMaxUsesSQL); err != nil {
		return fmt.Errorf("failed to ensure membership invite max_uses column: %w", err)
	}
	return nil
}
