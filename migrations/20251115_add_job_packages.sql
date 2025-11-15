-- Migration: Add job package booking feature
-- Date: 2025-11-15
-- Description: Allows packages to be booked to jobs as single line items while reserving underlying devices

-- Create job_packages table
CREATE TABLE IF NOT EXISTS `job_packages` (
  `job_package_id` bigint UNSIGNED NOT NULL AUTO_INCREMENT,
  `job_id` int NOT NULL,
  `package_id` int NOT NULL COMMENT 'References equipment_packages.packageID',
  `quantity` int UNSIGNED NOT NULL DEFAULT '1',
  `custom_price` decimal(12,2) DEFAULT NULL COMMENT 'Override package price for this job',
  `added_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `added_by` bigint UNSIGNED DEFAULT NULL,
  `notes` text COMMENT 'Special notes for this package assignment',
  PRIMARY KEY (`job_package_id`),
  KEY `idx_job_packages_job` (`job_id`),
  KEY `idx_job_packages_package` (`package_id`),
  KEY `idx_job_packages_added_by` (`added_by`),
  CONSTRAINT `job_packages_ibfk_1` FOREIGN KEY (`job_id`) REFERENCES `jobs` (`jobID`) ON DELETE CASCADE,
  CONSTRAINT `job_packages_ibfk_2` FOREIGN KEY (`package_id`) REFERENCES `equipment_packages` (`packageID`) ON DELETE RESTRICT,
  CONSTRAINT `job_packages_ibfk_3` FOREIGN KEY (`added_by`) REFERENCES `users` (`userID`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Packages assigned to jobs as single line items';

-- Create job_package_reservations table
CREATE TABLE IF NOT EXISTS `job_package_reservations` (
  `reservation_id` bigint UNSIGNED NOT NULL AUTO_INCREMENT,
  `job_package_id` bigint UNSIGNED NOT NULL COMMENT 'References job_packages',
  `device_id` varchar(50) NOT NULL COMMENT 'Reserved device',
  `quantity` int UNSIGNED NOT NULL DEFAULT '1' COMMENT 'Number of this device reserved',
  `reservation_status` enum('reserved','assigned','released') NOT NULL DEFAULT 'reserved',
  `reserved_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `assigned_at` timestamp NULL DEFAULT NULL,
  `released_at` timestamp NULL DEFAULT NULL,
  PRIMARY KEY (`reservation_id`),
  KEY `idx_job_pkg_res_job_package` (`job_package_id`),
  KEY `idx_job_pkg_res_device` (`device_id`),
  KEY `idx_job_pkg_res_status` (`reservation_status`),
  CONSTRAINT `job_package_reservations_ibfk_1` FOREIGN KEY (`job_package_id`) REFERENCES `job_packages` (`job_package_id`) ON DELETE CASCADE,
  CONSTRAINT `job_package_reservations_ibfk_2` FOREIGN KEY (`device_id`) REFERENCES `devices` (`deviceID`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Track device reservations for package assignments';
