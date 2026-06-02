ALTER TABLE users
ADD COLUMN IF NOT EXISTS daily_stock_analysis_user_id VARCHAR(128);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_dsa_user_id
ON users(daily_stock_analysis_user_id)
WHERE daily_stock_analysis_user_id IS NOT NULL;
