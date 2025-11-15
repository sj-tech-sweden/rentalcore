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

	// Verify package exists (from WarehouseCore product_packages)
	var pkg models.ProductPackage
	if err := tx.Where("package_id = ?", packageID).First(&pkg).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("package not found: %w", err)
	}

	// Get package items (from WarehouseCore product_package_items)
	var packageItems []models.ProductPackageItem
	if err := tx.Where("package_id = ?", packageID).Find(&packageItems).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to load package items: %w", err)
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

	// Check device availability (using product-based items)
	availabilityIssues := r.checkPackageItemAvailability(r.db, packageItems, quantity, startDate, endDate, jobID)
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

	// Create device reservations for each package item (by product)
	for _, pkgItem := range packageItems {
		totalQuantity := uint(pkgItem.Quantity) * quantity

		// Find available devices of this product type
		availableDevices, err := r.findAvailableDevicesByProduct(r.db, pkgItem.ProductID, totalQuantity, startDate, endDate, jobID)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to find available devices for product %d: %w", pkgItem.ProductID, err)
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

// checkPackageItemAvailability verifies all products in package are available
func (r *JobPackageRepository) checkPackageItemAvailability(tx *Database, packageItems []models.ProductPackageItem, quantity uint, startDate, endDate sql.NullTime, excludeJobID int) []string {
	var issues []string

	for _, pkgItem := range packageItems {
		totalNeeded := uint(pkgItem.Quantity) * quantity
		available, err := r.countAvailableDevicesByProduct(tx, pkgItem.ProductID, startDate, endDate, excludeJobID)
		if err != nil {
			issues = append(issues, fmt.Sprintf("Error checking product %d: %v", pkgItem.ProductID, err))
			continue
		}

		if available < totalNeeded {
			issues = append(issues, fmt.Sprintf("Product %d: need %d, only %d available", pkgItem.ProductID, totalNeeded, available))
		}
	}

	return issues
}

// countAvailableDevicesByProduct counts how many devices of a product are available
func (r *JobPackageRepository) countAvailableDevicesByProduct(tx *Database, productID int, startDate, endDate sql.NullTime, excludeJobID int) (uint, error) {
	// Count total devices of this product type
	var totalCount int64
	if err := tx.Model(&models.Device{}).Where("productID = ?", productID).Count(&totalCount).Error; err != nil {
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
		WHERE d.productID = ?
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
	if err := tx.Raw(query, productID, excludeJobID, endDate.Time, startDate.Time, excludeJobID, endDate.Time, startDate.Time).Scan(&reservedCount).Error; err != nil {
		return 0, err
	}

	available := totalCount - reservedCount
	if available < 0 {
		available = 0
	}

	return uint(available), nil
}

// findAvailableDevicesByProduct finds specific device instances by product that are available
func (r *JobPackageRepository) findAvailableDevicesByProduct(tx *Database, productID int, quantity uint, startDate, endDate sql.NullTime, excludeJobID int) ([]string, error) {
	var devices []string

	query := `
		SELECT d.deviceID
		FROM devices d
		WHERE d.productID = ?
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

	if err := tx.Raw(query, productID, excludeJobID, endDate.Time, startDate.Time, excludeJobID, endDate.Time, startDate.Time, quantity).Scan(&devices).Error; err != nil {
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
			if pkg.Package.Description.Valid {
				details.PackageDescription = pkg.Package.Description.String
			}
			if pkg.Package.Price.Valid {
				details.PackagePrice = pkg.Package.Price.Float64
			}

			// Count items in the package from product_package_items
			var itemCount int64
			r.db.Model(&models.ProductPackageItem{}).Where("package_id = ?", pkg.Package.PackageID).Count(&itemCount)
			details.DeviceCount = int(itemCount)
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
