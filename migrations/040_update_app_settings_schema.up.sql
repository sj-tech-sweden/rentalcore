-- Migration: update app_settings schema
-- Description: Add id (auto-increment PK) and scope columns to app_settings.
--              The scope column allows RentalCore and WarehouseCore to store
--              settings under different scopes while sharing the same table.
--              A unique constraint on (scope, key) replaces the old key-only PK.

-- Add scope column first (default 'global' so existing rows are migrated automatically)
ALTER TABLE app_settings ADD COLUMN IF NOT EXISTS scope VARCHAR(50) NOT NULL DEFAULT 'global';

-- Add id column as a nullable integer (backfill before enforcing NOT NULL / PK)
ALTER TABLE app_settings ADD COLUMN IF NOT EXISTS id INTEGER;

-- Backfill id for all existing rows using a sequence
CREATE SEQUENCE IF NOT EXISTS app_settings_id_seq;
UPDATE app_settings SET id = nextval('app_settings_id_seq') WHERE id IS NULL;

-- Now attach the sequence as the default for future inserts
ALTER TABLE app_settings ALTER COLUMN id SET DEFAULT nextval('app_settings_id_seq');
ALTER TABLE app_settings ALTER COLUMN id SET NOT NULL;

-- Transfer ownership of the sequence to the column (so it is dropped with the table)
ALTER SEQUENCE app_settings_id_seq OWNED BY app_settings.id;

-- Drop the old primary key constraint on key
ALTER TABLE app_settings DROP CONSTRAINT IF EXISTS app_settings_pkey;

-- Make id the new primary key
ALTER TABLE app_settings ADD PRIMARY KEY (id);

-- Add unique constraint on (scope, key) to prevent duplicates and support ON CONFLICT upserts
ALTER TABLE app_settings DROP CONSTRAINT IF EXISTS idx_app_settings_scope_key;
ALTER TABLE app_settings ADD CONSTRAINT idx_app_settings_scope_key UNIQUE (scope, key);
