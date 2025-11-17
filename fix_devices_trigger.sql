-- Fix devices trigger to allow virtual package devices
-- Run this on production database with root/admin privileges

USE RentalCore;

DROP TRIGGER IF EXISTS `devices`;

DELIMITER $$
CREATE TRIGGER `devices` BEFORE INSERT ON `devices` FOR EACH ROW device_trigger: BEGIN
  DECLARE abkuerzung   VARCHAR(50);
  DECLARE pos_cat       INT;
  DECLARE next_counter  INT;

  -- Skip auto-generation for virtual package devices (start with PKG_)
  IF NEW.deviceID IS NOT NULL AND NEW.deviceID LIKE 'PKG_%' THEN
    LEAVE device_trigger;
  END IF;

  -- 1) Abkürzung holen
  SELECT s.abbreviation
    INTO abkuerzung
    FROM subcategories s
    JOIN products      p ON s.subcategoryID = p.subcategoryID
   WHERE p.productID   = NEW.productID
   LIMIT 1;

  -- 2) pos_in_category holen
  SELECT p.pos_in_category
    INTO pos_cat
    FROM products p
   WHERE p.productID = NEW.productID;

  -- 3) Laufindex ermitteln (max. der letzten 3 Ziffern + 1)
  SELECT COALESCE(MAX(CAST(RIGHT(d.deviceID, 3) AS UNSIGNED)), 0) + 1
    INTO next_counter
    FROM devices d
   WHERE d.deviceID LIKE CONCAT(abkuerzung, pos_cat, '%');

  -- 4) deviceID zusammenbauen (ohne Bindestrich!)
  SET NEW.deviceID = CONCAT(
                        abkuerzung,
                        pos_cat,
                        LPAD(next_counter, 3, '0')
                      );
END$$
DELIMITER ;
