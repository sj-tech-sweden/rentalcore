package repository

import (
	"database/sql"
	"fmt"
	"go-barcode-webapp/internal/models"
	"time"
)

type JobPackageRepository struct {
	db *Database
}

func NewJobPackageRepository(db *Database) *JobPackageRepository {
	return &JobPackageRepository{db: db}
}

// AssignPackageToJob assigns a package to a job and creates device reservations
func (r *JobPackageRepository) AssignPackageToJob(jobID int, packageID int, quantity uint, customPrice *float64, userID uint) (*models.JobPackage, error) {
	// Start transaction
	tx := r.db.Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Verify package exists and is active
	var pkg models.EquipmentPackage
	if err := tx.Where("packageID = ? AND is_active = 1", packageID).First(&pkg).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("package not found or inactive: %w", err)
	}

	// Get package devices
	var packageDevices []models.PackageDevice
	if err := tx.Where("packageID = ?", packageID).Find(&packageDevices).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to load package devices: %w", err)
	}

	// Get job dates for availability check
	var job models.Job
	if err := tx.Where("jobID = ?", jobID).First(&job).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("job not found: %w", err)
	}

	// Convert time pointers to sql.NullTime
	var startDate, endDate sql.NullTime
	if job.StartDate != nil {
		startDate = sql.NullTime{Time: *job.StartDate, Valid: true}
	}
	if job.EndDate != nil {
		endDate = sql.NullTime{Time: *job.EndDate, Valid: true}
	}

	// Check device availability
	availabilityIssues := r.checkPackageDeviceAvailability(r.db, packageDevices, quantity, startDate, endDate, jobID)
	if len(availabilityIssues) > 0 {
		tx.Rollback()
		return nil, fmt.Errorf("device availability issues: %v", availabilityIssues)
	}

	// Create job package entry
	var priceValue sql.NullFloat64
	if customPrice != nil {
		priceValue = sql.NullFloat64{Float64: *customPrice, Valid: true}
	}

	jobPackage := &models.JobPackage{
		JobID:       jobID,
		PackageID:   packageID,
		Quantity:    quantity,
		CustomPrice: priceValue,
		AddedAt:     time.Now(),
		AddedBy:     &userID,
	}

	if err := tx.Create(jobPackage).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create job package: %w", err)
	}

	// Create device reservations for each package device
	for _, pkgDevice := range packageDevices {
		totalQuantity := pkgDevice.Quantity * quantity

		// Find available devices of this type
		availableDevices, err := r.findAvailableDevices(r.db, pkgDevice.DeviceID, totalQuantity, startDate, endDate, jobID)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to find available devices for %s: %w", pkgDevice.DeviceID, err)
		}

		// Create reservations
		for _, deviceID := range availableDevices {
			reservation := models.JobPackageReservation{
				JobPackageID:      jobPackage.JobPackageID,
				DeviceID:          deviceID,
				Quantity:          1,
				ReservationStatus: "reserved",
				ReservedAt:        time.Now(),
			}
			if err := tx.Create(&reservation).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to create reservation: %w", err)
			}
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Reload with associations
	return r.GetJobPackageByID(jobPackage.JobPackageID)
}

// checkPackageDeviceAvailability verifies all devices in package are available
func (r *JobPackageRepository) checkPackageDeviceAvailability(tx *Database, packageDevices []models.PackageDevice, quantity uint, startDate, endDate sql.NullTime, excludeJobID int) []string {
	var issues []string

	for _, pkgDevice := range packageDevices {
		totalNeeded := pkgDevice.Quantity * quantity
		available, err := r.countAvailableDevices(tx, pkgDevice.DeviceID, startDate, endDate, excludeJobID)
		if err != nil {
			issues = append(issues, fmt.Sprintf("Error checking %s: %v", pkgDevice.DeviceID, err))
			continue
		}

		if available < totalNeeded {
			issues = append(issues, fmt.Sprintf("Device %s: need %d, only %d available", pkgDevice.DeviceID, totalNeeded, available))
		}
	}

	return issues
}

// countAvailableDevices counts how many instances of a device are available
func (r *JobPackageRepository) countAvailableDevices(tx *Database, deviceIDPattern string, startDate, endDate sql.NullTime, excludeJobID int) (uint, error) {
	// Count total devices matching pattern
	var totalCount int64
	if err := tx.Model(&models.Device{}).Where("deviceID LIKE ?", deviceIDPattern+"%").Count(&totalCount).Error; err != nil {
		return 0, err
	}

	// If no date range, return total
	if !startDate.Valid || !endDate.Valid {
		return uint(totalCount), nil
	}

	// Count devices already reserved in overlapping jobs
	var reservedCount int64
	query := `
		SELECT COUNT(DISTINCT d.deviceID)
		FROM devices d
		WHERE d.deviceID LIKE ?
		AND (
			EXISTS (
				SELECT 1 FROM jobdevices jd
				JOIN jobs j ON jd.jobID = j.jobID
				WHERE jd.deviceID = d.deviceID
				AND j.jobID != ?
				AND j.start_date IS NOT NULL
				AND j.end_date IS NOT NULL
				AND j.start_date <= ?
				AND j.end_date >= ?
			)
			OR EXISTS (
				SELECT 1 FROM job_package_reservations jpr
				JOIN job_packages jp ON jpr.job_package_id = jp.job_package_id
				JOIN jobs j ON jp.job_id = j.jobID
				WHERE jpr.device_id = d.deviceID
				AND jpr.reservation_status = 'reserved'
				AND j.jobID != ?
				AND j.start_date IS NOT NULL
				AND j.end_date IS NOT NULL
				AND j.start_date <= ?
				AND j.end_date >= ?
			)
		)
	`
	if err := tx.Raw(query, deviceIDPattern+"%", excludeJobID, endDate.Time, startDate.Time, excludeJobID, endDate.Time, startDate.Time).Scan(&reservedCount).Error; err != nil {
		return 0, err
	}

	available := totalCount - reservedCount
	if available < 0 {
		available = 0
	}

	return uint(available), nil
}

// findAvailableDevices finds specific device instances that are available
func (r *JobPackageRepository) findAvailableDevices(tx *Database, deviceIDPattern string, quantity uint, startDate, endDate sql.NullTime, excludeJobID int) ([]string, error) {
	var devices []string

	query := `
		SELECT d.deviceID
		FROM devices d
		WHERE d.deviceID LIKE ?
		AND NOT EXISTS (
			SELECT 1 FROM jobdevices jd
			JOIN jobs j ON jd.jobID = j.jobID
			WHERE jd.deviceID = d.deviceID
			AND j.jobID != ?
			AND j.start_date IS NOT NULL
			AND j.end_date IS NOT NULL
			AND j.start_date <= ?
			AND j.end_date >= ?
		)
		AND NOT EXISTS (
			SELECT 1 FROM job_package_reservations jpr
			JOIN job_packages jp ON jpr.job_package_id = jp.job_package_id
			JOIN jobs j ON jp.job_id = j.jobID
			WHERE jpr.device_id = d.deviceID
			AND jpr.reservation_status = 'reserved'
			AND j.jobID != ?
			AND j.start_date IS NOT NULL
			AND j.end_date IS NOT NULL
			AND j.start_date <= ?
			AND j.end_date >= ?
		)
		LIMIT ?
	`

	if err := tx.Raw(query, deviceIDPattern+"%", excludeJobID, endDate.Time, startDate.Time, excludeJobID, endDate.Time, startDate.Time, quantity).Scan(&devices).Error; err != nil {
		return nil, err
	}

	if len(devices) < int(quantity) {
		return nil, fmt.Errorf("insufficient available devices: need %d, found %d", quantity, len(devices))
	}

	return devices, nil
}

// GetJobPackageByID retrieves a job package by ID with associations
func (r *JobPackageRepository) GetJobPackageByID(id uint) (*models.JobPackage, error) {
	var jobPackage models.JobPackage
	err := r.db.
		Preload("Package").
		Preload("Package.PackageDevices").
		Preload("Reservations").
		Preload("Reservations.Device").
		Preload("AddedByUser").
		Where("job_package_id = ?", id).
		First(&jobPackage).Error

	if err != nil {
		return nil, err
	}

	return &jobPackage, nil
}

// GetJobPackagesByJobID retrieves all packages for a job
func (r *JobPackageRepository) GetJobPackagesByJobID(jobID int) ([]models.JobPackage, error) {
	var packages []models.JobPackage
	err := r.db.
		Preload("Package").
		Preload("Package.PackageDevices").
		Preload("Package.PackageDevices.Device").
		Preload("Reservations").
		Preload("Reservations.Device").
		Preload("AddedByUser").
		Where("job_id = ?", jobID).
		Order("added_at DESC").
		Find(&packages).Error

	return packages, err
}

// UpdateJobPackageQuantity updates the quantity of a job package
func (r *JobPackageRepository) UpdateJobPackageQuantity(jobPackageID uint, newQuantity uint) error {
	// This would require re-calculating reservations
	// For now, we'll implement a simple version
	return r.db.Model(&models.JobPackage{}).
		Where("job_package_id = ?", jobPackageID).
		Update("quantity", newQuantity).Error
}

// UpdateJobPackagePrice updates the custom price
func (r *JobPackageRepository) UpdateJobPackagePrice(jobPackageID uint, newPrice *float64) error {
	var priceValue sql.NullFloat64
	if newPrice != nil {
		priceValue = sql.NullFloat64{Float64: *newPrice, Valid: true}
	} else {
		priceValue = sql.NullFloat64{Valid: false}
	}

	return r.db.Model(&models.JobPackage{}).
		Where("job_package_id = ?", jobPackageID).
		Update("custom_price", priceValue).Error
}

// RemoveJobPackage removes a package from a job and releases reservations
func (r *JobPackageRepository) RemoveJobPackage(jobPackageID uint) error {
	tx := r.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Update reservation status to released
	if err := tx.Model(&models.JobPackageReservation{}).
		Where("job_package_id = ?", jobPackageID).
		Updates(map[string]interface{}{
			"reservation_status": "released",
			"released_at":        time.Now(),
		}).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Delete the job package (cascades to reservations via DB constraints)
	if err := tx.Delete(&models.JobPackage{}, jobPackageID).Error; err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

// GetPackageDeviceReservations retrieves all device reservations for a package
func (r *JobPackageRepository) GetPackageDeviceReservations(jobPackageID uint) ([]models.JobPackageReservation, error) {
	var reservations []models.JobPackageReservation
	err := r.db.
		Preload("Device").
		Preload("Device.Product").
		Where("job_package_id = ?", jobPackageID).
		Order("reserved_at").
		Find(&reservations).Error

	return reservations, err
}

// ReleasePackageReservations releases all device reservations for a package
func (r *JobPackageRepository) ReleasePackageReservations(jobPackageID uint) error {
	return r.db.Model(&models.JobPackageReservation{}).
		Where("job_package_id = ? AND reservation_status = 'reserved'", jobPackageID).
		Updates(map[string]interface{}{
			"reservation_status": "released",
			"released_at":        time.Now(),
		}).Error
}

// GetJobPackagesWithDetails retrieves packages with computed details for display
func (r *JobPackageRepository) GetJobPackagesWithDetails(jobID int) ([]models.JobPackageWithDetails, error) {
	var packages []models.JobPackage
	err := r.db.
		Preload("Package").
		Preload("Package.PackageDevices").
		Preload("Reservations").
		Where("job_id = ?", jobID).
		Order("added_at DESC").
		Find(&packages).Error

	if err != nil {
		return nil, err
	}

	result := make([]models.JobPackageWithDetails, len(packages))
	for i, pkg := range packages {
		details := models.JobPackageWithDetails{
			JobPackage: pkg,
		}

		if pkg.Package != nil {
			details.PackageName = pkg.Package.Name
			details.PackageDescription = pkg.Package.Description
			if pkg.Package.PackagePrice != nil {
				details.PackagePrice = *pkg.Package.PackagePrice
			}
			details.DeviceCount = len(pkg.Package.PackageDevices)
		}

		// Calculate effective price
		if pkg.CustomPrice.Valid {
			details.EffectivePrice = pkg.CustomPrice.Float64
		} else {
			details.EffectivePrice = details.PackagePrice
		}

		details.TotalPrice = details.EffectivePrice * float64(pkg.Quantity)
		details.ReservedDevices = len(pkg.Reservations)

		// Determine availability status
		if details.ReservedDevices >= details.DeviceCount*int(pkg.Quantity) {
			details.AvailabilityStatus = "fully_reserved"
		} else if details.ReservedDevices > 0 {
			details.AvailabilityStatus = "partially_reserved"
		} else {
			details.AvailabilityStatus = "not_reserved"
		}

		result[i] = details
	}

	return result, nil
}
