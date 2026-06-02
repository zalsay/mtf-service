-- 迁移脚本：将 user_watchlist 从 stock_id 改为 symbol

-- Step 1: 添加新的 symbol 列
ALTER TABLE user_watchlist ADD COLUMN IF NOT EXISTS symbol VARCHAR(20);

-- Step 2: 从 stocks 表填充 symbol 数据（如果有现有数据）
UPDATE user_watchlist uw
SET symbol = s.symbol
FROM stocks s
WHERE uw.stock_id = s.id AND uw.symbol IS NULL;

-- Step 3: 删除旧的 stock_id 列的外键约束
ALTER TABLE user_watchlist DROP CONSTRAINT IF EXISTS user_watchlist_stock_id_fkey;
ALTER TABLE user_watchlist DROP COLUMN IF EXISTS stock_id;

-- Step 4: 设置 symbol 为 NOT NULL
ALTER TABLE user_watchlist ALTER COLUMN symbol SET NOT NULL;

-- Step 5: 添加新的唯一约束
ALTER TABLE user_watchlist DROP CONSTRAINT IF EXISTS user_watchlist_user_id_stock_id_key;
ALTER TABLE user_watchlist ADD CONSTRAINT user_watchlist_user_id_symbol_unique UNIQUE (user_id, symbol);

-- Step 6: 添加索引
CREATE INDEX IF NOT EXISTS idx_watchlist_symbol ON user_watchlist(symbol);
