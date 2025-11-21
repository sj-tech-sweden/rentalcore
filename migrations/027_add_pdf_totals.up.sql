-- Add total fields to pdf_extractions table
ALTER TABLE pdf_extractions
    ADD COLUMN parsed_total DECIMAL(10, 2) NULL COMMENT 'Subtotal before discount',
    ADD COLUMN discount_percent DECIMAL(5, 2) NULL COMMENT 'Discount percentage';

-- Note: discount_amount and total_amount columns already exist
