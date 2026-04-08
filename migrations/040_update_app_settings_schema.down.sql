-- Rollback: revert app_settings schema changes
ALTER TABLE app_settings DROP CONSTRAINT IF EXISTS idx_app_settings_scope_key;
ALTER TABLE app_settings DROP CONSTRAINT IF EXISTS app_settings_pkey;
-- Drop scope before re-adding key-only PK to avoid duplicate keys across scopes
ALTER TABLE app_settings DROP COLUMN IF EXISTS scope;
ALTER TABLE app_settings ADD PRIMARY KEY (key);
ALTER TABLE app_settings DROP COLUMN IF EXISTS id;
