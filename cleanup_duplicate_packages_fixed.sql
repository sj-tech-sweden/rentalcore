-- Cleanup duplicate package assignments
-- This script removes duplicate job_package entries and keeps only the earliest one per job+package combination

USE RentalCore;

-- Step 1: Delete duplicate JobDevices entries for package items
DELETE jd FROM jobdevices jd
INNER JOIN (
    SELECT jd2.jobID, jd2.deviceID, MIN(jd2.jobID) as min_jobID
    FROM jobdevices jd2
    WHERE jd2.is_package_item = 1
      AND jd2.package_id IS NOT NULL
    GROUP BY jd2.jobID, jd2.deviceID, jd2.package_id
    HAVING COUNT(*) > 1
) dups ON jd.jobID = dups.jobID AND jd.deviceID = dups.deviceID
WHERE jd.is_package_item = 1
  AND jd.package_id IS NOT NULL
  AND jd.jobID > dups.min_jobID;

-- Step 2: Delete duplicate virtual package devices
DELETE jd FROM jobdevices jd
INNER JOIN (
    SELECT jd2.jobID, jd2.deviceID
    FROM jobdevices jd2
    WHERE jd2.deviceID LIKE 'PKG_%'
      AND jd2.is_package_item = 0
    GROUP BY jd2.jobID, jd2.deviceID
    HAVING COUNT(*) > 1
) dups ON jd.jobID = dups.jobID AND jd.deviceID = dups.deviceID
WHERE jd.deviceID LIKE 'PKG_%'
  AND jd.is_package_item = 0;

-- Step 3: Delete job_package_reservations for duplicate packages
DELETE jpr FROM job_package_reservations jpr
INNER JOIN (
    SELECT jp2.job_package_id
    FROM job_packages jp1
    INNER JOIN job_packages jp2
      ON jp1.job_id = jp2.job_id
      AND jp1.package_id = jp2.package_id
      AND jp1.job_package_id < jp2.job_package_id
) dups ON jpr.job_package_id = dups.job_package_id;

-- Step 4: Keep only the earliest job_package entry for each job+package combination
DELETE jp1 FROM job_packages jp1
INNER JOIN job_packages jp2
  ON jp1.job_id = jp2.job_id
  AND jp1.package_id = jp2.package_id
  AND jp1.job_package_id > jp2.job_package_id;

-- Step 5: Show summary
SELECT
    'Job Packages' as table_name,
    COUNT(*) as remaining_count
FROM job_packages
UNION ALL
SELECT
    'Job Devices (Package Items)' as table_name,
    COUNT(*) as remaining_count
FROM jobdevices
WHERE is_package_item = 1
UNION ALL
SELECT
    'Job Devices (Virtual Packages)' as table_name,
    COUNT(*) as remaining_count
FROM jobdevices
WHERE deviceID LIKE 'PKG_%';
