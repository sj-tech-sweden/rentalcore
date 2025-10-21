ALTER TABLE `jobs`
  ADD COLUMN `job_code` VARCHAR(16) NULL;

UPDATE `jobs`
SET `job_code` = CONCAT('JOB', LPAD(`jobID`, 6, '0'))
WHERE `job_code` IS NULL OR `job_code` = '';

ALTER TABLE `jobs`
  MODIFY COLUMN `job_code` VARCHAR(16) NOT NULL,
  ADD UNIQUE KEY `ux_jobs_job_code` (`job_code`);
