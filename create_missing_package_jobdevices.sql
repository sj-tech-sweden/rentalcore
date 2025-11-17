-- Create missing JobDevices for packages
-- This script creates virtual devices and real device entries for packages that are missing them

USE RentalCore;

-- Step 1: Create virtual devices for packages (one per job_package)
INSERT INTO devices (deviceID, productID, status, notes)
SELECT
    CONCAT('PKG_', jp.job_package_id) as deviceID,
    (1000000 + jp.package_id) as productID,
    'package_virtual' as status,
    CONCAT('Package: ', pp.name, ' (ID: ', jp.package_id, ', Quantity: ', jp.quantity, ')') as notes
FROM job_packages jp
LEFT JOIN product_packages pp ON jp.package_id = pp.package_id
WHERE NOT EXISTS (
    SELECT 1 FROM devices d
    WHERE d.deviceID = CONCAT('PKG_', jp.job_package_id)
);

-- Step 2: Create virtual JobDevice entries (for package in product list)
INSERT INTO jobdevices (jobID, deviceID, custom_price, package_id, is_package_item)
SELECT
    jp.job_id as jobID,
    CONCAT('PKG_', jp.job_package_id) as deviceID,
    jp.custom_price,
    NULL as package_id,
    0 as is_package_item
FROM job_packages jp
WHERE NOT EXISTS (
    SELECT 1 FROM jobdevices jd
    WHERE jd.jobID = jp.job_id
      AND jd.deviceID = CONCAT('PKG_', jp.job_package_id)
);

-- Step 3: Create real device JobDevice entries (for warehouse scans)
INSERT INTO jobdevices (jobID, deviceID, custom_price, package_id, is_package_item)
SELECT
    jp.job_id as jobID,
    jpr.device_id as deviceID,
    0.00 as custom_price,
    jp.package_id,
    1 as is_package_item
FROM job_package_reservations jpr
JOIN job_packages jp ON jpr.job_package_id = jp.job_package_id
WHERE NOT EXISTS (
    SELECT 1 FROM jobdevices jd
    WHERE jd.jobID = jp.job_id
      AND jd.deviceID = jpr.device_id
      AND jd.is_package_item = 1
);

-- Step 4: Show summary
SELECT
    'Virtual Package Devices Created' as action,
    COUNT(*) as count
FROM devices
WHERE deviceID LIKE 'PKG_%'
  AND status = 'package_virtual'
UNION ALL
SELECT
    'Virtual Package JobDevices Created' as action,
    COUNT(*) as count
FROM jobdevices
WHERE deviceID LIKE 'PKG_%'
  AND is_package_item = 0
UNION ALL
SELECT
    'Real Package Item JobDevices Created' as action,
    COUNT(*) as count
FROM jobdevices
WHERE is_package_item = 1;
