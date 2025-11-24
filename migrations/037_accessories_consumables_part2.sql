-- ============================================================================
-- Migration #037 Part 2: Complete accessories/consumables feature
-- Missing parts: inventory_transactions, views, and triggers
-- ============================================================================

-- ============================================================================
-- 1. Inventory Transactions Table
-- ============================================================================
CREATE TABLE IF NOT EXISTS `inventory_transactions` (
  `transaction_id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `product_id` INT NOT NULL COMMENT 'FK to products (accessories/consumables)',
  `transaction_type` ENUM('in', 'out', 'adjustment', 'initial') NOT NULL COMMENT 'Type of transaction',
  `quantity` DECIMAL(10,3) NOT NULL COMMENT 'Quantity (positive or negative)',
  `reference_type` VARCHAR(50) DEFAULT NULL COMMENT 'e.g., job, purchase, manual',
  `reference_id` INT DEFAULT NULL COMMENT 'ID of related entity (job_id, etc.)',
  `notes` TEXT COMMENT 'Transaction notes',
  `user_id` BIGINT UNSIGNED DEFAULT NULL COMMENT 'User who performed transaction',
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`transaction_id`),
  KEY `idx_inventory_trans_product` (`product_id`),
  KEY `idx_inventory_trans_type` (`transaction_type`),
  KEY `idx_inventory_trans_reference` (`reference_type`, `reference_id`),
  KEY `idx_inventory_trans_created` (`created_at`),
  CONSTRAINT `fk_inventory_trans_product` FOREIGN KEY (`product_id`) REFERENCES `products` (`productID`) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT `fk_inventory_trans_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`userID`) ON DELETE SET NULL ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
COMMENT='Tracks all inventory movements for accessories and consumables';

-- ============================================================================
-- 2. Create Views
-- ============================================================================

-- View: Product Accessories Detail
CREATE OR REPLACE VIEW `vw_product_accessories` AS
SELECT
    pa.product_id,
    p.name AS product_name,
    pa.accessory_product_id,
    ap.name AS accessory_name,
    ap.stock_quantity AS accessory_stock,
    ap.price_per_unit AS accessory_price,
    ct.name AS count_type,
    ct.abbreviation AS count_type_abbr,
    pa.is_optional,
    pa.default_quantity,
    pa.sort_order,
    ap.generic_barcode
FROM product_accessories pa
INNER JOIN products p ON pa.product_id = p.productID
INNER JOIN products ap ON pa.accessory_product_id = ap.productID
LEFT JOIN count_types ct ON ap.count_type_id = ct.count_type_id
WHERE ap.is_accessory = 1
ORDER BY pa.product_id, pa.sort_order, ap.name;

-- View: Product Consumables Detail
CREATE OR REPLACE VIEW `vw_product_consumables` AS
SELECT
    pc.product_id,
    p.name AS product_name,
    pc.consumable_product_id,
    cp.name AS consumable_name,
    cp.stock_quantity AS consumable_stock,
    cp.price_per_unit AS consumable_price,
    ct.name AS count_type,
    ct.abbreviation AS count_type_abbr,
    pc.default_quantity,
    pc.sort_order,
    cp.generic_barcode
FROM product_consumables pc
INNER JOIN products p ON pc.product_id = p.productID
INNER JOIN products cp ON pc.consumable_product_id = cp.productID
LEFT JOIN count_types ct ON cp.count_type_id = ct.count_type_id
WHERE cp.is_consumable = 1
ORDER BY pc.product_id, pc.sort_order, cp.name;

-- View: Job Accessories with Stock Status
CREATE OR REPLACE VIEW `vw_job_accessories_detail` AS
SELECT
    ja.job_accessory_id,
    ja.job_id,
    j.description AS job_description,
    ja.parent_device_id,
    d.productID AS parent_product_id,
    p.name AS parent_product_name,
    ja.accessory_product_id,
    ap.name AS accessory_name,
    ja.quantity_assigned,
    ja.quantity_scanned_out,
    ja.quantity_scanned_in,
    (ja.quantity_assigned - ja.quantity_scanned_out) AS quantity_pending_out,
    (ja.quantity_scanned_out - ja.quantity_scanned_in) AS quantity_pending_in,
    ja.price_per_unit,
    (ja.quantity_assigned * COALESCE(ja.price_per_unit, ap.price_per_unit, 0)) AS total_price,
    ct.name AS count_type,
    ct.abbreviation AS count_type_abbr,
    ap.generic_barcode,
    ja.notes,
    ja.created_at,
    ja.updated_at
FROM job_accessories ja
INNER JOIN jobs j ON ja.job_id = j.jobID
LEFT JOIN devices d ON ja.parent_device_id = d.deviceID
LEFT JOIN products p ON d.productID = p.productID
INNER JOIN products ap ON ja.accessory_product_id = ap.productID
LEFT JOIN count_types ct ON ap.count_type_id = ct.count_type_id
ORDER BY ja.job_id, ja.job_accessory_id;

-- View: Job Consumables with Stock Status
CREATE OR REPLACE VIEW `vw_job_consumables_detail` AS
SELECT
    jc.job_consumable_id,
    jc.job_id,
    j.description AS job_description,
    jc.parent_device_id,
    d.productID AS parent_product_id,
    p.name AS parent_product_name,
    jc.consumable_product_id,
    cp.name AS consumable_name,
    jc.quantity_assigned,
    jc.quantity_scanned_out,
    jc.quantity_scanned_in,
    (jc.quantity_assigned - jc.quantity_scanned_out) AS quantity_pending_out,
    (jc.quantity_scanned_out - jc.quantity_scanned_in) AS quantity_pending_in,
    jc.price_per_unit,
    (jc.quantity_assigned * COALESCE(jc.price_per_unit, cp.price_per_unit, 0)) AS total_price,
    ct.name AS count_type,
    ct.abbreviation AS count_type_abbr,
    cp.generic_barcode,
    jc.notes,
    jc.created_at,
    jc.updated_at
FROM job_consumables jc
INNER JOIN jobs j ON jc.job_id = j.jobID
LEFT JOIN devices d ON jc.parent_device_id = d.deviceID
LEFT JOIN products p ON d.productID = p.productID
INNER JOIN products cp ON jc.consumable_product_id = cp.productID
LEFT JOIN count_types ct ON cp.count_type_id = ct.count_type_id
ORDER BY jc.job_id, jc.job_consumable_id;

-- View: Low Stock Alert
CREATE OR REPLACE VIEW `vw_low_stock_alert` AS
SELECT
    p.productID,
    p.name,
    p.stock_quantity,
    p.min_stock_level,
    (p.min_stock_level - p.stock_quantity) AS quantity_below_min,
    ct.name AS count_type,
    ct.abbreviation AS count_type_abbr,
    p.generic_barcode,
    CASE
        WHEN p.is_accessory = 1 THEN 'Accessory'
        WHEN p.is_consumable = 1 THEN 'Consumable'
        ELSE 'Unknown'
    END AS item_type
FROM products p
LEFT JOIN count_types ct ON p.count_type_id = ct.count_type_id
WHERE (p.is_accessory = 1 OR p.is_consumable = 1)
  AND p.stock_quantity <= COALESCE(p.min_stock_level, 0)
ORDER BY (p.min_stock_level - p.stock_quantity) DESC;

-- ============================================================================
-- 3. Triggers for automatic stock management
-- ============================================================================

-- Drop existing triggers if they exist
DROP TRIGGER IF EXISTS `trg_job_accessories_scan_out`;
DROP TRIGGER IF EXISTS `trg_job_accessories_scan_in`;
DROP TRIGGER IF EXISTS `trg_job_consumables_scan_out`;
DROP TRIGGER IF EXISTS `trg_job_consumables_scan_in`;

-- Trigger: Decrease stock when accessories scanned out
DELIMITER $$
CREATE TRIGGER `trg_job_accessories_scan_out`
AFTER UPDATE ON `job_accessories`
FOR EACH ROW
BEGIN
    IF NEW.quantity_scanned_out > OLD.quantity_scanned_out THEN
        -- Decrease stock
        UPDATE products
        SET stock_quantity = stock_quantity - (NEW.quantity_scanned_out - OLD.quantity_scanned_out)
        WHERE productID = NEW.accessory_product_id;

        -- Log transaction
        INSERT INTO inventory_transactions
        (product_id, transaction_type, quantity, reference_type, reference_id, notes)
        VALUES
        (NEW.accessory_product_id, 'out', (NEW.quantity_scanned_out - OLD.quantity_scanned_out), 'job', NEW.job_id, 'Scanned out for job');
    END IF;
END$$

-- Trigger: Increase stock when accessories scanned in
CREATE TRIGGER `trg_job_accessories_scan_in`
AFTER UPDATE ON `job_accessories`
FOR EACH ROW
BEGIN
    IF NEW.quantity_scanned_in > OLD.quantity_scanned_in THEN
        -- Increase stock
        UPDATE products
        SET stock_quantity = stock_quantity + (NEW.quantity_scanned_in - OLD.quantity_scanned_in)
        WHERE productID = NEW.accessory_product_id;

        -- Log transaction
        INSERT INTO inventory_transactions
        (product_id, transaction_type, quantity, reference_type, reference_id, notes)
        VALUES
        (NEW.accessory_product_id, 'in', (NEW.quantity_scanned_in - OLD.quantity_scanned_in), 'job', NEW.job_id, 'Scanned in from job');
    END IF;
END$$

-- Trigger: Decrease stock when consumables scanned out
CREATE TRIGGER `trg_job_consumables_scan_out`
AFTER UPDATE ON `job_consumables`
FOR EACH ROW
BEGIN
    IF NEW.quantity_scanned_out > OLD.quantity_scanned_out THEN
        -- Decrease stock
        UPDATE products
        SET stock_quantity = stock_quantity - (NEW.quantity_scanned_out - OLD.quantity_scanned_out)
        WHERE productID = NEW.consumable_product_id;

        -- Log transaction
        INSERT INTO inventory_transactions
        (product_id, transaction_type, quantity, reference_type, reference_id, notes)
        VALUES
        (NEW.consumable_product_id, 'out', (NEW.quantity_scanned_out - OLD.quantity_scanned_out), 'job', NEW.job_id, 'Scanned out for job');
    END IF;
END$$

-- Trigger: Increase stock when consumables scanned in
CREATE TRIGGER `trg_job_consumables_scan_in`
AFTER UPDATE ON `job_consumables`
FOR EACH ROW
BEGIN
    IF NEW.quantity_scanned_in > OLD.quantity_scanned_in THEN
        -- Increase stock
        UPDATE products
        SET stock_quantity = stock_quantity + (NEW.quantity_scanned_in - OLD.quantity_scanned_in)
        WHERE productID = NEW.consumable_product_id;

        -- Log transaction
        INSERT INTO inventory_transactions
        (product_id, transaction_type, quantity, reference_type, reference_id, notes)
        VALUES
        (NEW.consumable_product_id, 'in', (NEW.quantity_scanned_in - OLD.quantity_scanned_in), 'job', NEW.job_id, 'Scanned in from job');
    END IF;
END$$
DELIMITER ;

-- ============================================================================
-- Migration Part 2 Complete
-- ============================================================================
