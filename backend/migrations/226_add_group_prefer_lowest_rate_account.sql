ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS prefer_lowest_rate_account BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN groups.prefer_lowest_rate_account IS
    '调度时优先选择计费倍率最低的可用账号';
