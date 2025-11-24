# Accessories and Consumables API Documentation

## Overview

The accessories and consumables feature allows products to have optional accessories and required consumables that can be assigned to jobs. This system supports inventory tracking with different measurement units (kg, pieces, liters, etc.).

## Key Concepts

- **Accessories**: Optional items that can be added to products (e.g., safety cables for lights)
- **Consumables**: Items that are consumed during jobs (e.g., fog fluid, tape, cable ties)
- **Count Types**: Measurement units for tracking quantities (pieces, kg, liters, etc.)
- **Generic Barcode**: Single barcode for all units of an accessory/consumable type
- **Inventory Tracking**: Automatic stock updates when items are scanned in/out

## API Endpoints

### Count Types

#### GET /api/count-types
Get all active measurement units.

**Response:**
```json
{
  "count_types": [
    {
      "count_type_id": 1,
      "name": "Piece",
      "abbreviation": "pcs",
      "is_active": true
    },
    {
      "count_type_id": 2,
      "name": "Kilogram",
      "abbreviation": "kg",
      "is_active": true
    }
  ]
}
```

### Product Accessories

#### GET /api/products/:productID/accessories
Get all accessories linked to a product.

**Response:**
```json
{
  "accessories": [
    {
      "product_id": 100,
      "product_name": "LED Par",
      "accessory_product_id": 500,
      "accessory_name": "Safety Cable 40kg",
      "accessory_stock": 50.0,
      "accessory_price": 2.50,
      "count_type": "Piece",
      "count_type_abbr": "pcs",
      "is_optional": true,
      "default_quantity": 1,
      "sort_order": 1,
      "generic_barcode": "ACC-SAFE40"
    }
  ]
}
```

#### POST /api/products/:productID/accessories
Add an accessory to a product.

**Request:**
```json
{
  "product_id": 100,
  "accessory_product_id": 500,
  "is_optional": true,
  "default_quantity": 1,
  "sort_order": 1
}
```

#### DELETE /api/products/:productID/accessories/:accessoryID
Remove an accessory from a product.

### Product Consumables

#### GET /api/products/:productID/consumables
Get all consumables linked to a product.

**Response:**
```json
{
  "consumables": [
    {
      "product_id": 200,
      "product_name": "Fog Machine",
      "consumable_product_id": 600,
      "consumable_name": "Fog Fluid",
      "consumable_stock": 25.5,
      "consumable_price": 12.00,
      "count_type": "Kilogram",
      "count_type_abbr": "kg",
      "default_quantity": 5.0,
      "sort_order": 1,
      "generic_barcode": "CONS-FOG"
    }
  ]
}
```

#### POST /api/products/:productID/consumables
Add a consumable to a product.

**Request:**
```json
{
  "product_id": 200,
  "consumable_product_id": 600,
  "default_quantity": 5.0,
  "sort_order": 1
}
```

#### DELETE /api/products/:productID/consumables/:consumableID
Remove a consumable from a product.

### Accessory and Consumable Products

#### GET /api/accessories/products
Get all products marked as accessories.

**Response:**
```json
{
  "products": [
    {
      "productID": 500,
      "name": "Safety Cable 40kg",
      "is_accessory": true,
      "count_type_id": 1,
      "stock_quantity": 50.0,
      "min_stock_level": 10.0,
      "generic_barcode": "ACC-SAFE40",
      "price_per_unit": 2.50,
      "count_type": {
        "name": "Piece",
        "abbreviation": "pcs"
      }
    }
  ]
}
```

#### GET /api/consumables/products
Get all products marked as consumables.

### Job Accessories

#### GET /api/jobs/:jobID/accessories
Get all accessories assigned to a job.

**Response:**
```json
{
  "accessories": [
    {
      "job_accessory_id": 1,
      "job_id": 10001,
      "parent_device_id": "DEV-001",
      "accessory_product_id": 500,
      "quantity_assigned": 4,
      "quantity_scanned_out": 4,
      "quantity_scanned_in": 0,
      "price_per_unit": 2.50,
      "accessory_product": {
        "name": "Safety Cable 40kg",
        "generic_barcode": "ACC-SAFE40"
      }
    }
  ]
}
```

#### POST /api/jobs/:jobID/accessories
Assign accessories to a job.

**Request:**
```json
{
  "job_id": 10001,
  "parent_device_id": "DEV-001",
  "accessory_product_id": 500,
  "quantity_assigned": 4,
  "price_per_unit": 2.50
}
```

#### PUT /api/jobs/accessories/:id
Update a job accessory assignment.

#### DELETE /api/jobs/accessories/:id
Remove an accessory assignment from a job.

### Job Consumables

#### GET /api/jobs/:jobID/consumables
Get all consumables assigned to a job.

#### POST /api/jobs/:jobID/consumables
Assign consumables to a job.

**Request:**
```json
{
  "job_id": 10001,
  "parent_device_id": "DEV-002",
  "consumable_product_id": 600,
  "quantity_assigned": 5.5,
  "price_per_unit": 12.00
}
```

#### PUT /api/jobs/consumables/:id
Update a job consumable assignment.

#### DELETE /api/jobs/consumables/:id
Remove a consumable assignment from a job.

### Inventory Management

#### GET /api/inventory/low-stock
Get products below minimum stock level.

**Response:**
```json
{
  "alerts": [
    {
      "productID": 500,
      "name": "Safety Cable 40kg",
      "stock_quantity": 5.0,
      "min_stock_level": 10.0,
      "quantity_below_min": 5.0,
      "count_type": "Piece",
      "count_type_abbr": "pcs",
      "generic_barcode": "ACC-SAFE40",
      "item_type": "Accessory"
    }
  ]
}
```

#### POST /api/inventory/adjust
Manually adjust stock levels.

**Request:**
```json
{
  "product_id": 500,
  "quantity": 10.0,
  "reason": "Received new shipment"
}
```

#### GET /api/inventory/transactions
Get inventory transaction history.

**Query Parameters:**
- `product_id` (required): Product ID
- `limit` (optional, default: 50): Number of records

**Response:**
```json
{
  "transactions": [
    {
      "transaction_id": 1,
      "product_id": 500,
      "transaction_type": "out",
      "quantity": 4.0,
      "reference_type": "job",
      "reference_id": 10001,
      "notes": "Scanned out for job",
      "created_at": "2025-11-24T10:00:00Z"
    }
  ]
}
```

### Barcode Scanning (WarehouseCore Integration)

#### POST /api/scan/accessory
Scan an accessory barcode for a job.

**Request:**
```json
{
  "barcode": "ACC-SAFE40",
  "job_id": 10001,
  "direction": "out",
  "quantity": 1
}
```

**Direction values:**
- `"out"`: Scanning out for a job (decreases stock)
- `"in"`: Scanning in from a job (increases stock)

**Response:**
```json
{
  "message": "Accessory scanned successfully",
  "product": {
    "productID": 500,
    "name": "Safety Cable 40kg"
  },
  "quantity": 1,
  "remaining_stock": 49.0
}
```

#### POST /api/scan/consumable
Scan a consumable barcode for a job.

**Request:**
```json
{
  "barcode": "CONS-FOG",
  "job_id": 10001,
  "direction": "out",
  "quantity": 5.5
}
```

**Response:**
```json
{
  "message": "Consumable scanned successfully",
  "product": {
    "productID": 600,
    "name": "Fog Fluid"
  },
  "quantity": 5.5,
  "remaining_stock": 20.0
}
```

## Workflow Examples

### Adding Accessories to a Product

1. Create/identify an accessory product (marked with `is_accessory = true`)
2. Link it to the main product: `POST /api/products/100/accessories`
3. When adding the product to a job, optionally add accessories: `POST /api/jobs/10001/accessories`

### Scanning Accessories Out for a Job

1. Scan the generic barcode (e.g., "ACC-SAFE40")
2. API call: `POST /api/scan/accessory` with `direction: "out"`
3. System automatically:
   - Decreases stock quantity
   - Updates job accessory scanned_out count
   - Logs inventory transaction

### Scanning Consumables Back In

1. Scan the generic barcode
2. User enters quantity being returned
3. API call: `POST /api/scan/consumable` with `direction: "in"` and quantity
4. System automatically:
   - Increases stock quantity
   - Updates job consumable scanned_in count
   - Logs inventory transaction

## Error Handling

All endpoints return standard HTTP status codes:
- `200 OK`: Successful operation
- `201 Created`: Successfully created resource
- `400 Bad Request`: Invalid input data
- `404 Not Found`: Resource not found
- `500 Internal Server Error`: Server error

Error response format:
```json
{
  "error": "Error message description"
}
```

## Notes

- Accessories are scanned one by one (each scan = 1 piece)
- Consumables require quantity input when scanning
- Stock is automatically tracked through scanning operations
- Inventory transactions provide full audit trail
- Low stock alerts help with reordering
