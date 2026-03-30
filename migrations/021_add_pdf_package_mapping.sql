-- Migration 021: Add package mapping support for OCR extraction items

ALTER TABLE IF EXISTS pdf_extraction_items
    ADD COLUMN IF NOT EXISTS mapped_package_id INT;

CREATE INDEX IF NOT EXISTS idx_pdf_items_package
    ON pdf_extraction_items(mapped_package_id);

DO $$
BEGIN
    IF to_regclass('public.pdf_extraction_items') IS NOT NULL
       AND to_regclass('public.product_packages') IS NOT NULL THEN
        BEGIN
            ALTER TABLE pdf_extraction_items
                ADD CONSTRAINT fk_pdf_items_package
                FOREIGN KEY (mapped_package_id)
                REFERENCES product_packages(id)
                ON DELETE SET NULL;
        EXCEPTION
            WHEN duplicate_object THEN NULL;
        END;
    END IF;
END $$;
