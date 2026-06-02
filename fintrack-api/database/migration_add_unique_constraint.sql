-- 为现有的 user_sessions 表添加 UNIQUE 约束
-- 执行前先清理重复的 session 记录

-- Step 1: 删除每个用户的旧 session，只保留最新的一条
DELETE FROM user_sessions
WHERE id NOT IN (
    SELECT MAX(id)
    FROM user_sessions
    GROUP BY user_id
);

-- Step 2: 添加 UNIQUE 约束
ALTER TABLE user_sessions 
ADD CONSTRAINT user_sessions_user_id_unique UNIQUE (user_id);
