-- Migration: update app_settings schema
-- Description: Add scope and id columns to app_settings.
--              The scope column allows RentalCore and WarehouseCore to store
--              settings under different scopes while sharing the same table.
--              A unique constraint on (scope, key) replaces the old key-only PK.

-- Step 1: Add scope column without NOT NULL so it can be added to existing tables with data,
--         then backfill any NULLs and enforce NOT NULL afterwards.
ALTER TABLE app_settings ADD COLUMN IF NOT EXISTS scope VARCHAR(50) DEFAULT 'global';
UPDATE app_settings SET scope = 'global' WHERE scope IS NULL;
ALTER TABLE app_settings ALTER COLUMN scope SET DEFAULT 'global';
ALTER TABLE app_settings ALTER COLUMN scope SET NOT NULL;

-- Step 2: Add id column as a nullable integer initially (must backfill before enforcing NOT NULL / PK)
ALTER TABLE app_settings ADD COLUMN IF NOT EXISTS id INTEGER;

-- Step 3: Backfill id for all existing rows using a sequence
CREATE SEQUENCE IF NOT EXISTS app_settings_id_seq;
UPDATE app_settings SET id = nextval('app_settings_id_seq') WHERE id IS NULL;

-- Step 3a: Advance the sequence to MAX(id) so future inserts never conflict with backfilled values
SELECT setval('app_settings_id_seq', COALESCE((SELECT MAX(id) FROM app_settings), 1), true);

-- Step 4: Attach the sequence as the default for future inserts and enforce NOT NULL
ALTER TABLE app_settings ALTER COLUMN id SET DEFAULT nextval('app_settings_id_seq');
ALTER TABLE app_settings ALTER COLUMN id SET NOT NULL;

-- Step 5: Transfer sequence ownership so it is dropped with the table
ALTER SEQUENCE app_settings_id_seq OWNED BY app_settings.id;

-- Step 6: Drop the old primary key constraint on key
ALTER TABLE app_settings DROP CONSTRAINT IF EXISTS app_settings_pkey;

-- Step 7: Make id the new primary key
ALTER TABLE app_settings ADD PRIMARY KEY (id);

-- Step 8: Add unique constraint on (scope, key) to prevent duplicates and support ON CONFLICT upserts
--         Drop both the constraint and the index (GORM AutoMigrate creates an index, not a named constraint)
--         to ensure the ADD CONSTRAINT succeeds regardless of how the unique relation was previously created.
ALTER TABLE app_settings DROP CONSTRAINT IF EXISTS idx_app_settings_scope_key;
DROP INDEX IF EXISTS idx_app_settings_scope_key;
ALTER TABLE app_settings ADD CONSTRAINT idx_app_settings_scope_key UNIQUE (scope, key);
