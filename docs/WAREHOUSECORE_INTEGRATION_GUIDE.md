# WarehouseCore Integration Guide
## Accessories & Consumables Scanning System

**Version**: 1.0
**Date**: 2025-11-24
**For**: WarehouseCore Development Team

---

## 📋 Overview

This guide provides complete implementation instructions for integrating the Accessories & Consumables system into WarehouseCore. All backend APIs in RentalCore are ready and functional. WarehouseCore needs to implement:

1. Product form extensions (mark products as accessories/consumables)
2. Barcode scanning interface for generic barcodes
3. Quantity input prompts for consumables

---

## 🎯 Implementation Checklist

### **Phase 1: Product Management** (Priority: High)

- [ ] Extend product creation form with accessory/consumable fields
- [ ] Add inventory tracking fields to product edit interface
- [ ] Implement product type selector (standard/accessory/consumable)
- [ ] Add count type dropdown (pieces, kg, liters, etc.)
- [ ] Add stock quantity and minimum stock level fields
- [ ] Add generic barcode field with validation

### **Phase 2: Scanning Interface** (Priority: High)

- [ ] Update scanner to detect generic barcodes
- [ ] Implement accessory scanning workflow (one scan per piece)
- [ ] Implement consumable scanning workflow (scan + quantity prompt)
- [ ] Add real-time stock display after each scan
- [ ] Integrate with RentalCore scan APIs

### **Phase 3: Admin Features** (Priority: Medium)

- [ ] Create "Accessories" and "Consumables" categories
- [ ] Add bulk import for accessories/consumables
- [ ] Create stock adjustment interface
- [ ] Implement low stock alert notifications

---

## 🛠️ Part 1: Product Form Extensions

### Current State
- Products are managed in WarehouseCore
- Product database table already has all required fields
- Fields were added in RentalCore migration `037_accessories_consumables.sql`

### Required Implementation

#### 1.1 Product Type Selection

Add checkboxes to the product form:

```html
<div class="form-group">
    <h4>Product Type</h4>
    <div class="checkbox-group">
        <label>
            <input type="checkbox" id="is_accessory" name="is_accessory">
            <span>This is an Accessory</span>
        </label>
        <label>
            <input type="checkbox" id="is_consumable" name="is_consumable">
            <span>This is a Consumable</span>
        </label>
    </div>
    <p class="help-text">
        Accessories are optional items (cables, clamps).
        Consumables are used items (fog fluid, tape).
    </p>
</div>
```

#### 1.2 Inventory Fields (Conditional Display)

Show these fields when either checkbox is checked:

```html
<div id="inventory-fields" style="display: none;">
    <!-- Count Type (Measurement Unit) -->
    <div class="form-group">
        <label for="count_type_id">Measurement Unit *</label>
        <select id="count_type_id" name="count_type_id" required>
            <option value="">Select unit...</option>
            <!-- Populated from RentalCore API: GET /api/count-types -->
        </select>
    </div>

    <!-- Generic Barcode -->
    <div class="form-group">
        <label for="generic_barcode">Generic Barcode</label>
        <input type="text" id="generic_barcode" name="generic_barcode"
               placeholder="e.g., ACC-SAFE40, CONS-FOG">
        <p class="help-text">Single barcode for all units of this type</p>
    </div>

    <!-- Stock Quantity -->
    <div class="form-group">
        <label for="stock_quantity">Current Stock Quantity</label>
        <input type="number" id="stock_quantity" name="stock_quantity"
               step="0.001" min="0" value="0">
    </div>

    <!-- Minimum Stock Level -->
    <div class="form-group">
        <label for="min_stock_level">Minimum Stock Level</label>
        <input type="number" id="min_stock_level" name="min_stock_level"
               step="0.001" min="0" value="0">
        <p class="help-text">Alert when stock falls below this level</p>
    </div>

    <!-- Price Per Unit -->
    <div class="form-group">
        <label for="price_per_unit">Price per Unit (€)</label>
        <input type="number" id="price_per_unit" name="price_per_unit"
               step="0.01" min="0" value="0">
    </div>
</div>
```

#### 1.3 JavaScript Toggle Logic

```javascript
// Show/hide inventory fields based on checkboxes
document.getElementById('is_accessory').addEventListener('change', toggleInventoryFields);
document.getElementById('is_consumable').addEventListener('change', toggleInventoryFields);

function toggleInventoryFields() {
    const isAccessory = document.getElementById('is_accessory').checked;
    const isConsumable = document.getElementById('is_consumable').checked;
    const inventoryFields = document.getElementById('inventory-fields');

    if (isAccessory || isConsumable) {
        inventoryFields.style.display = 'block';
        document.getElementById('count_type_id').required = true;
    } else {
        inventoryFields.style.display = 'none';
        document.getElementById('count_type_id').required = false;
    }
}
```

#### 1.4 Load Count Types from API

```javascript
async function loadCountTypes() {
    try {
        const res = await fetch('http://rentalcore:8081/api/count-types');
        const data = await res.json();

        const select = document.getElementById('count_type_id');
        select.innerHTML = '<option value="">Select unit...</option>';

        (data.count_types || []).forEach(ct => {
            const option = document.createElement('option');
            option.value = ct.count_type_id;
            option.textContent = `${ct.name} (${ct.abbreviation})`;
            select.appendChild(option);
        });
    } catch (error) {
        console.error('Error loading count types:', error);
    }
}

// Call on page load
document.addEventListener('DOMContentLoaded', loadCountTypes);
```

#### 1.5 Form Submission

When saving the product, include the new fields in the payload:

```javascript
const productData = {
    // ... existing fields ...
    is_accessory: document.getElementById('is_accessory').checked,
    is_consumable: document.getElementById('is_consumable').checked,
    count_type_id: parseInt(document.getElementById('count_type_id').value) || null,
    stock_quantity: parseFloat(document.getElementById('stock_quantity').value) || 0,
    min_stock_level: parseFloat(document.getElementById('min_stock_level').value) || 0,
    generic_barcode: document.getElementById('generic_barcode').value || null,
    price_per_unit: parseFloat(document.getElementById('price_per_unit').value) || 0
};
```

---

## 📱 Part 2: Scanning Interface Implementation

### 2.1 Barcode Detection Logic

Update your existing barcode scanner to detect generic barcodes:

```javascript
async function handleBarcodeScan(barcode, jobID, direction) {
    // direction: "out" (checkout) or "in" (return)

    // Try to find product by generic barcode
    const product = await findProductByGenericBarcode(barcode);

    if (product) {
        if (product.is_accessory) {
            await handleAccessoryScan(barcode, jobID, direction);
        } else if (product.is_consumable) {
            await handleConsumableScan(barcode, jobID, direction);
        }
    } else {
        // Fall back to standard device barcode handling
        handleStandardDeviceScan(barcode, jobID, direction);
    }
}

async function findProductByGenericBarcode(barcode) {
    try {
        // Query local database or RentalCore API
        const res = await fetch(`http://rentalcore:8081/api/products?barcode=${barcode}`);
        const data = await res.json();
        return data.product || null;
    } catch (error) {
        console.error('Error finding product:', error);
        return null;
    }
}
```

### 2.2 Accessory Scanning Workflow

**User Story**: Scan the same barcode multiple times (once per piece)

```javascript
async function handleAccessoryScan(barcode, jobID, direction) {
    try {
        const res = await fetch('http://rentalcore:8081/api/scan/accessory', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                barcode: barcode,
                job_id: jobID,
                direction: direction, // "out" or "in"
                quantity: 1 // Always 1 for accessories (scan per piece)
            })
        });

        if (!res.ok) {
            const error = await res.json();
            showError(error.error || 'Scan failed');
            return;
        }

        const data = await res.json();

        // Show success notification
        showSuccessNotification(
            `✅ Scanned 1x ${data.product.name}`,
            `Remaining stock: ${data.remaining_stock} pcs`
        );

        // Play success beep
        playBeep();

        // Update UI if needed
        updateJobScanStatus(jobID);
    } catch (error) {
        console.error('Error scanning accessory:', error);
        showError('Network error during scan');
    }
}
```

**Example Flow for Scanning Out**:
1. Job #10001 requires 4x Safety Cable 40kg
2. User scans "ACC-SAFE40" → Notification: "✅ Scanned 1x Safety Cable 40kg (49 remaining)"
3. User scans "ACC-SAFE40" → Notification: "✅ Scanned 2x Safety Cable 40kg (48 remaining)"
4. User scans "ACC-SAFE40" → Notification: "✅ Scanned 3x Safety Cable 40kg (47 remaining)"
5. User scans "ACC-SAFE40" → Notification: "✅ Scanned 4x Safety Cable 40kg (46 remaining)"
6. All 4 pieces scanned ✓

### 2.3 Consumable Scanning Workflow

**User Story**: Scan once, then prompt for quantity

```javascript
async function handleConsumableScan(barcode, jobID, direction) {
    try {
        // Find the product details
        const product = await findProductByGenericBarcode(barcode);

        if (!product) {
            showError('Product not found');
            return;
        }

        // Prompt user for quantity
        const quantity = await promptForQuantity(
            product.name,
            product.count_type.abbreviation,
            product.stock_quantity
        );

        if (quantity === null) {
            return; // User cancelled
        }

        // Submit scan with quantity
        const res = await fetch('http://rentalcore:8081/api/scan/consumable', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                barcode: barcode,
                job_id: jobID,
                direction: direction, // "out" or "in"
                quantity: parseFloat(quantity)
            })
        });

        if (!res.ok) {
            const error = await res.json();
            showError(error.error || 'Scan failed');
            return;
        }

        const data = await res.json();

        // Show success notification
        showSuccessNotification(
            `✅ Scanned ${quantity}${product.count_type.abbreviation} ${data.product.name}`,
            `Remaining stock: ${data.remaining_stock} ${product.count_type.abbreviation}`
        );

        // Play success beep
        playBeep();

        // Update UI
        updateJobScanStatus(jobID);
    } catch (error) {
        console.error('Error scanning consumable:', error);
        showError('Network error during scan');
    }
}
```

### 2.4 Quantity Input Modal

Create a modal for quantity input:

```html
<div id="quantityInputModal" class="modal" style="display: none;">
    <div class="modal-content">
        <div class="modal-header">
            <h3 id="quantityModalTitle">Enter Quantity</h3>
            <button class="modal-close" onclick="closeQuantityModal()">×</button>
        </div>
        <div class="modal-body">
            <p id="quantityModalDescription"></p>
            <div class="form-group">
                <label for="quantityInput">Quantity</label>
                <input type="number" id="quantityInput" step="0.001" min="0.001"
                       placeholder="0.000" autofocus>
                <span id="quantityUnit"></span>
            </div>
            <p class="help-text" id="quantityAvailable"></p>
        </div>
        <div class="modal-footer">
            <button class="btn btn-ghost" onclick="closeQuantityModal()">Cancel</button>
            <button class="btn btn-primary" onclick="submitQuantity()">Confirm</button>
        </div>
    </div>
</div>
```

JavaScript for the modal:

```javascript
function promptForQuantity(productName, unit, availableStock) {
    return new Promise((resolve) => {
        const modal = document.getElementById('quantityInputModal');
        const input = document.getElementById('quantityInput');

        // Set modal content
        document.getElementById('quantityModalTitle').textContent = `Enter Quantity for ${productName}`;
        document.getElementById('quantityModalDescription').textContent =
            `How much ${productName} are you scanning?`;
        document.getElementById('quantityUnit').textContent = unit;
        document.getElementById('quantityAvailable').textContent =
            `Available: ${availableStock} ${unit}`;

        // Reset input
        input.value = '';
        input.max = availableStock;

        // Show modal
        modal.style.display = 'flex';
        input.focus();

        // Store resolve function
        window.__quantityResolve = resolve;
    });
}

function submitQuantity() {
    const quantity = parseFloat(document.getElementById('quantityInput').value);

    if (!quantity || quantity <= 0) {
        alert('Please enter a valid quantity');
        return;
    }

    closeQuantityModal();

    if (window.__quantityResolve) {
        window.__quantityResolve(quantity);
    }
}

function closeQuantityModal() {
    document.getElementById('quantityInputModal').style.display = 'none';

    if (window.__quantityResolve) {
        window.__quantityResolve(null); // User cancelled
    }
}

// Allow Enter key to submit
document.getElementById('quantityInput').addEventListener('keypress', function(e) {
    if (e.key === 'Enter') {
        submitQuantity();
    }
});
```

**Example Flow for Scanning Out**:
1. Job #10001 requires 5.5kg Fog Fluid
2. User scans "CONS-FOG"
3. Modal appears: "Enter Quantity for Fog Fluid"
4. User enters "5.5"
5. User clicks "Confirm"
6. Notification: "✅ Scanned 5.5kg Fog Fluid (Remaining: 19.5kg)"

### 2.5 Real-Time Stock Display

After each scan, update the UI to show:
- Current stock level
- Assigned vs scanned quantities for the current job
- Visual progress indicator

```javascript
async function updateJobScanStatus(jobID) {
    try {
        // Fetch current status
        const [accRes, consRes] = await Promise.all([
            fetch(`http://rentalcore:8081/api/jobs/${jobID}/accessories`),
            fetch(`http://rentalcore:8081/api/jobs/${jobID}/consumables`)
        ]);

        const accData = await accRes.json();
        const consData = await consRes.json();

        // Update UI with scan status
        displayScanProgress(accData.accessories, consData.consumables);
    } catch (error) {
        console.error('Error updating scan status:', error);
    }
}

function displayScanProgress(accessories, consumables) {
    // Example: Show progress for each item
    const container = document.getElementById('scan-progress');

    let html = '<h4>Scan Progress</h4>';

    accessories.forEach(acc => {
        const progress = (acc.quantity_scanned_out / acc.quantity_assigned) * 100;
        html += `
            <div class="scan-item">
                <span>${acc.accessory_product.name}</span>
                <span>${acc.quantity_scanned_out}/${acc.quantity_assigned}</span>
                <div class="progress-bar">
                    <div class="progress-fill" style="width: ${progress}%"></div>
                </div>
            </div>
        `;
    });

    consumables.forEach(cons => {
        const progress = (cons.quantity_scanned_out / cons.quantity_assigned) * 100;
        html += `
            <div class="scan-item">
                <span>${cons.consumable_product.name}</span>
                <span>${cons.quantity_scanned_out.toFixed(1)}/${cons.quantity_assigned.toFixed(1)}</span>
                <div class="progress-bar">
                    <div class="progress-fill" style="width: ${progress}%"></div>
                </div>
            </div>
        `;
    });

    container.innerHTML = html;
}
```

---

## 🔧 Part 3: Admin Features

### 3.1 Category Management

Create special categories for accessories and consumables:

```javascript
// Add these categories to your category management system
const specialCategories = [
    { name: 'Accessories', type: 'accessory', icon: 'paperclip' },
    { name: 'Consumables', type: 'consumable', icon: 'droplet' }
];

// When displaying products, filter by category
async function loadAccessoryProducts() {
    const res = await fetch('http://rentalcore:8081/api/accessories/products');
    const data = await res.json();
    displayProducts(data.products);
}

async function loadConsumableProducts() {
    const res = await fetch('http://rentalcore:8081/api/consumables/products');
    const data = await res.json();
    displayProducts(data.products);
}
```

### 3.2 Low Stock Notifications

Display low stock alerts in WarehouseCore dashboard:

```javascript
async function loadLowStockAlerts() {
    try {
        const res = await fetch('http://rentalcore:8081/api/inventory/low-stock');
        const data = await res.json();

        const alerts = data.alerts || [];

        if (alerts.length > 0) {
            showLowStockBanner(alerts);
        }
    } catch (error) {
        console.error('Error loading low stock alerts:', error);
    }
}

function showLowStockBanner(alerts) {
    const banner = document.createElement('div');
    banner.className = 'alert alert-warning';
    banner.innerHTML = `
        <i class="bi bi-exclamation-triangle"></i>
        <strong>Low Stock Alert:</strong>
        ${alerts.length} item(s) below minimum stock level.
        <a href="/inventory/low-stock">View Details</a>
    `;

    document.querySelector('.dashboard-header').appendChild(banner);
}
```

### 3.3 Stock Adjustment Interface

Create a form for manual stock adjustments:

```html
<div class="stock-adjustment-form">
    <h3>Manual Stock Adjustment</h3>
    <form id="adjustStockForm">
        <div class="form-group">
            <label for="product_select">Product</label>
            <select id="product_select" required>
                <!-- Populated with accessories/consumables -->
            </select>
        </div>
        <div class="form-group">
            <label for="adjustment_quantity">Adjustment Quantity</label>
            <input type="number" id="adjustment_quantity" step="0.001" required>
            <p class="help-text">Use negative for decrease, positive for increase</p>
        </div>
        <div class="form-group">
            <label for="adjustment_reason">Reason</label>
            <textarea id="adjustment_reason" required></textarea>
        </div>
        <button type="submit" class="btn btn-primary">Adjust Stock</button>
    </form>
</div>
```

Submit adjustment:

```javascript
document.getElementById('adjustStockForm').addEventListener('submit', async (e) => {
    e.preventDefault();

    const productID = document.getElementById('product_select').value;
    const quantity = parseFloat(document.getElementById('adjustment_quantity').value);
    const reason = document.getElementById('adjustment_reason').value;

    try {
        const res = await fetch('http://rentalcore:8081/api/inventory/adjust', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                product_id: parseInt(productID),
                quantity: quantity,
                reason: reason
            })
        });

        if (res.ok) {
            alert('Stock adjusted successfully');
            loadProductStock(productID); // Refresh display
        } else {
            const error = await res.json();
            alert('Error: ' + error.error);
        }
    } catch (error) {
        console.error('Error adjusting stock:', error);
        alert('Network error');
    }
});
```

---

## 📊 Part 4: UI/UX Guidelines

### Visual Design

1. **Accessories Icon**: 📎 (paperclip icon)
2. **Consumables Icon**: 💧 (droplet icon)
3. **Color Coding**:
   - Accessories: Blue accent (#3b82f6)
   - Consumables: Green accent (#10b981)
   - Low Stock: Orange/Red (#f59e0b / #ef4444)

### Notifications

Use toast notifications for scan feedback:

```javascript
function showSuccessNotification(title, message) {
    const toast = document.createElement('div');
    toast.className = 'toast toast-success';
    toast.innerHTML = `
        <div class="toast-icon">✓</div>
        <div class="toast-content">
            <strong>${title}</strong>
            <p>${message}</p>
        </div>
    `;

    document.body.appendChild(toast);

    setTimeout(() => {
        toast.classList.add('show');
    }, 100);

    setTimeout(() => {
        toast.classList.remove('show');
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

function showError(message) {
    const toast = document.createElement('div');
    toast.className = 'toast toast-error';
    toast.innerHTML = `
        <div class="toast-icon">⚠</div>
        <div class="toast-content">
            <strong>Error</strong>
            <p>${message}</p>
        </div>
    `;

    document.body.appendChild(toast);

    setTimeout(() => {
        toast.classList.add('show');
    }, 100);

    setTimeout(() => {
        toast.classList.remove('show');
        setTimeout(() => toast.remove(), 300);
    }, 5000);
}
```

### Sound Feedback

Add audio cues for scan events:

```javascript
const beepSuccess = new Audio('/sounds/beep-success.mp3');
const beepError = new Audio('/sounds/beep-error.mp3');

function playBeep(success = true) {
    if (success) {
        beepSuccess.play();
    } else {
        beepError.play();
    }
}
```

---

## 🔌 API Reference (RentalCore Endpoints)

All endpoints are ready and functional in RentalCore.

### Authentication

All API calls to RentalCore should include authentication headers if required:

```javascript
const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}` // If using token auth
};
```

### Count Types

```
GET /api/count-types
Response: {
    "count_types": [
        { "count_type_id": 1, "name": "Piece", "abbreviation": "pcs" },
        { "count_type_id": 2, "name": "Kilogram", "abbreviation": "kg" },
        ...
    ]
}
```

### Product Lists

```
GET /api/accessories/products
GET /api/consumables/products
Response: {
    "products": [...]
}
```

### Scanning

```
POST /api/scan/accessory
Body: {
    "barcode": "ACC-SAFE40",
    "job_id": 10001,
    "direction": "out",  // or "in"
    "quantity": 1
}
Response: {
    "message": "Accessory scanned successfully",
    "product": { "productID": 500, "name": "Safety Cable 40kg" },
    "quantity": 1,
    "remaining_stock": 49
}
```

```
POST /api/scan/consumable
Body: {
    "barcode": "CONS-FOG",
    "job_id": 10001,
    "direction": "out",
    "quantity": 5.5
}
Response: {
    "message": "Consumable scanned successfully",
    "product": { "productID": 600, "name": "Fog Fluid" },
    "quantity": 5.5,
    "remaining_stock": 19.5
}
```

### Inventory Management

```
GET /api/inventory/low-stock
POST /api/inventory/adjust
GET /api/inventory/transactions?product_id=500&limit=50
```

### Job Accessories/Consumables

```
GET /api/jobs/:jobID/accessories
GET /api/jobs/:jobID/consumables
POST /api/jobs/:jobID/accessories
POST /api/jobs/:jobID/consumables
```

Full API documentation: See `/opt/dev/cores/rentalcore/docs/API_ACCESSORIES_CONSUMABLES.md`

---

## 🧪 Testing Scenarios

### Scenario 1: Create Accessory Product

1. Open product form in WarehouseCore
2. Check "This is an Accessory"
3. Select "Piece (pcs)" as measurement unit
4. Enter generic barcode: "ACC-TEST-001"
5. Set stock: 100, min stock: 20
6. Set price: €5.00
7. Save product
8. Verify in RentalCore inventory dashboard

### Scenario 2: Scan Accessory Out

1. Open job scanning interface
2. Select Job #10001
3. Scan barcode "ACC-TEST-001"
4. Verify notification: "✅ Scanned 1x Test Accessory"
5. Verify stock decreased to 99
6. Scan again 3 more times
7. Verify job shows "4/0 scanned"

### Scenario 3: Scan Consumable Out

1. Open job scanning interface
2. Select Job #10001
3. Scan barcode "CONS-TEST-001"
4. Modal appears: "Enter Quantity for Test Consumable"
5. Enter "2.5" kg
6. Verify notification: "✅ Scanned 2.5kg Test Consumable"
7. Verify stock decreased by 2.5kg
8. Verify job shows "2.5/0.0 scanned"

### Scenario 4: Low Stock Alert

1. Set min stock level to 10 for a product
2. Scan out items until stock reaches 9
3. Verify low stock alert appears in WarehouseCore dashboard
4. Verify alert shows in RentalCore inventory dashboard
5. Perform stock adjustment to increase stock
6. Verify alert disappears

---

## 📞 Support & Questions

For technical questions or issues:
- **Backend APIs**: See `docs/API_ACCESSORIES_CONSUMABLES.md`
- **Database Schema**: See `migrations/037_accessories_consumables*.sql`
- **Handler Code**: See `internal/handlers/accessories_consumables_handler.go`
- **Repository Code**: See `internal/repository/accessories_consumables_repository.go`

---

## ✅ Implementation Completion Criteria

WarehouseCore implementation is complete when:

1. ✅ Products can be marked as accessories/consumables with all required fields
2. ✅ Generic barcodes can be scanned for accessories (one scan per piece)
3. ✅ Generic barcodes can be scanned for consumables (with quantity prompt)
4. ✅ Real-time stock updates are reflected after each scan
5. ✅ Low stock alerts are displayed in WarehouseCore dashboard
6. ✅ Stock adjustments can be performed with reason tracking
7. ✅ All scan operations successfully communicate with RentalCore APIs
8. ✅ Transaction history is accessible and accurate

---

**Last Updated**: 2025-11-24
**Version**: 1.0
**RentalCore Version**: 4.1.37+
