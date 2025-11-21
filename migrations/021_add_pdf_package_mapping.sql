-- Migration 021: Add package mapping support for OCR extraction items

ALTER TABLE pdf_extraction_items
    ADD COLUMN mapped_package_id INT NULL AFTER mapped_product_id,
    ADD KEY idx_pdf_items_package (mapped_package_id);

ALTER TABLE pdf_extraction_items
    ADD CONSTRAINT fk_pdf_items_package
        FOREIGN KEY (mapped_package_id)
        REFERENCES product_packages(package_id)
        ON DELETE SET NULL;
