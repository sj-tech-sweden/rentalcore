-- Migration: app_settings table
-- Description: Shared key-value settings table used by both RentalCore and
--              WarehouseCore. Setting the same key (e.g. app.currency) in either
--              application will affect both, provided they share the same database.

CREATE TABLE IF NOT EXISTS app_settings (
    key        VARCHAR(128) NOT NULL,
    value      TEXT         NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (key)
);

-- Seed the default currency symbol so existing installations are consistent.
INSERT INTO app_settings (key, value)
VALUES ('app.currency', '€')
ON CONFLICT (key) DO NOTHING;
