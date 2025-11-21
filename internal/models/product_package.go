package models

import (
	"database/sql"
	"time"
)

// ProductPackage mirrors the shared WarehouseCore product_packages table
type ProductPackage struct {
	PackageID   int             `gorm:"primaryKey;column:package_id" json:"package_id"`
	ProductID   int             `gorm:"column:product_id" json:"product_id"`        // Links to products table
	PackageCode string          `gorm:"column:package_code" json:"package_code"`
	Name        string          `gorm:"column:name" json:"name"`
	Description sql.NullString  `gorm:"column:description" json:"description"`
	Price       sql.NullFloat64 `gorm:"column:price" json:"price"`
	CreatedAt   time.Time       `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time       `gorm:"column:updated_at" json:"updated_at"`
}

// TableName overrides the default table name for GORM
func (ProductPackage) TableName() string {
	return "product_packages"
}

// ProductPackageItem maps a product to a package with a fixed quantity
type ProductPackageItem struct {
	PackageItemID int `gorm:"primaryKey;column:package_item_id" json:"package_item_id"`
	PackageID     int `gorm:"column:package_id" json:"package_id"`
	ProductID     int `gorm:"column:product_id" json:"product_id"`
	Quantity      int `gorm:"column:quantity" json:"quantity"`
}

// TableName overrides the table for GORM
func (ProductPackageItem) TableName() string {
	return "product_package_items"
}
