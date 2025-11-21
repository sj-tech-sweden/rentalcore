-- ========================================
-- v4.0 Migration Cleanup Script
-- Removes all virtual package devices and products
-- from the old package system
-- ========================================

-- Show what will be deleted (for verification)
SELECT '=== VIRTUAL JOBDEVICES TO DELETE ===' AS info;
SELECT deviceID, jobID FROM jobdevices WHERE deviceID LIKE 'PKG_%';

SELECT '=== VIRTUAL DEVICES TO DELETE ===' AS info;
SELECT deviceID, productID, status, notes FROM devices
WHERE deviceID LIKE 'PKG_%' OR status = 'package_virtual';

SELECT '=== VIRTUAL PRODUCTS TO DELETE ===' AS info;
SELECT productID, name, itemcostperday FROM products
WHERE productID >= 1000000;

-- Start transaction for safety
START TRANSACTION;

-- 1. Delete JobDevices entries for virtual package devices
DELETE FROM jobdevices
WHERE deviceID LIKE 'PKG_%';

SELECT '✓ Deleted virtual JobDevices' AS status, ROW_COUNT() AS deleted_count;

-- 2. Delete virtual package devices
DELETE FROM devices
WHERE deviceID LIKE 'PKG_%' OR status = 'package_virtual';

SELECT '✓ Deleted virtual Devices' AS status, ROW_COUNT() AS deleted_count;

-- 3. Delete virtual package products (productID >= 1000000)
DELETE FROM products
WHERE productID >= 1000000;

SELECT '✓ Deleted virtual Products' AS status, ROW_COUNT() AS deleted_count;

-- 4. Optional: Clean up old job_package_reservations (from old system)
-- Uncomment if you want to remove these as well
-- DELETE FROM job_package_reservations;
-- SELECT '✓ Deleted job_package_reservations' AS status, ROW_COUNT() AS deleted_count;

-- 5. Optional: Clean up job_packages table (from old system)
-- Uncomment if you want to remove these as well
-- DELETE FROM job_packages;
-- SELECT '✓ Deleted job_packages' AS status, ROW_COUNT() AS deleted_count;

-- Verify cleanup
SELECT '=== CLEANUP VERIFICATION ===' AS info;

SELECT 'Virtual JobDevices remaining:' AS check_type,
       COUNT(*) AS count
FROM jobdevices
WHERE deviceID LIKE 'PKG_%';

SELECT 'Virtual Devices remaining:' AS check_type,
       COUNT(*) AS count
FROM devices
WHERE deviceID LIKE 'PKG_%' OR status = 'package_virtual';

SELECT 'Virtual Products remaining:' AS check_type,
       COUNT(*) AS count
FROM products
WHERE productID >= 1000000;

-- COMMIT or ROLLBACK
-- Uncomment ONE of the following:

-- To apply changes:
COMMIT;
SELECT '✅ CLEANUP COMPLETED - Changes committed' AS final_status;

-- To undo changes (if something looks wrong):
-- ROLLBACK;
-- SELECT '❌ CLEANUP ROLLED BACK - No changes made' AS final_status;
