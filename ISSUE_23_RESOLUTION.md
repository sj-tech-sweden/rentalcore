# Issue #23 Resolution: Product Tree Assignment Logic

## Status: ✅ FULLY RESOLVED (2025-11-03)

All features requested in Issue #23 are now fully implemented and match the exact requirements.

**Fixed in version 2.49:**
- UI layout corrected to show availability BELOW input field (as specified in issue)

## Requested Features vs. Implementation

### 1. Product Tree UI (Categories → Subcategories → Products)
**Status:** ✅ Implemented

**Location:** `web/templates/job_form.html:264-304`

The job form includes a complete product tree interface that displays:
- Categories
- Subcategories
- Subbiercategories (sub-subcategories)
- Products (NOT individual devices)

**Code Reference:**
```html
<!-- Product Selection Section -->
<div class="rc-card rc-mb-lg">
    <div class="rc-card-header">
        <h4 class="rc-card-title">
            <i class="bi bi-box-seam"></i> Product Selection
        </h4>
    </div>
    ...
</div>
```

### 2. Manual Quantity Input with Availability Display
**Status:** ✅ Implemented & Fixed (v2.49)

**Location:** `web/templates/job_form.html:985-1019`

**Layout (Fixed):**
```
[Product Name]           [Input Field]
                         [6 / 12 Badge]
```

Each product shows:
- Product name
- Manual input field for quantity selection
- Available quantity / Total quantity displayed BELOW input (e.g., "6 / 12")
- Color-coded badges (green = available, yellow/orange = low stock)

**Fix Details (commit 962e797):**
- Changed from 3-column to 2-column grid layout
- Moved availability badge below input field
- Input container now uses vertical flex layout
- Matches exact requirement: "darunter steht die verfügbare anzahl" (below it shows the available quantity)

**Frontend JavaScript:**
```javascript
function renderProductEntry(product) {
    const available = product.AvailableCount || 0;
    const total = product.DeviceCount || 0;
    // Displays: <span class="rc-badge">6 / 12</span>
    ...
}
```

### 3. Quantity Validation with Error Messages
**Status:** ✅ Implemented

**Location:** `web/templates/job_form.html:1095-1122`

When quantity exceeds availability:
- Real-time validation as you type
- Error message: "[Product Name]: requested X, available Y"
- Form submission is blocked
- User must adjust quantities before submitting

**Code Reference:**
```javascript
function validateProductSelections() {
    productValidationErrors = [];
    productSelections.forEach(entry => {
        if (entry.quantity > entry.available) {
            productValidationErrors.push(
                `${entry.name}: requested ${entry.quantity}, available ${entry.available}`
            );
        }
    });
    // Display error box if validation fails
}
```

### 4. Smart Case-Aware Device Allocation Logic
**Status:** ✅ FULLY IMPLEMENTED

**Location:** `internal/handlers/job_handler.go:793-910`

**Function:** `resolveProductSelections()`

This implements the EXACT logic described in the issue!

**Algorithm:**
1. Group all available devices by their case ID
2. Sort cases by size (largest first)
3. Allocate devices from full cases FIRST
4. Only split cases when necessary
5. Use loose devices (not in cases) as last resort

**Example from Issue #23:**
> Request: 8 Akkukannen
> Available: 2 cases with 6 each
> Result: Takes 6 from Case A + 2 from Case B (NOT 4+4)

**Code Excerpt:**
```go
// Sort cases by size (largest first) to prefer full cases
sort.Slice(caseOrder, func(i, j int) bool {
    return len(caseGroups[caseOrder[i]]) > len(caseGroups[caseOrder[j]])
})

// Allocate from cases first (lines 866-885)
for _, caseID := range caseOrder {
    if remaining == 0 {
        break
    }
    devices := caseGroups[caseID]
    for _, device := range devices {
        if remaining == 0 {
            break
        }
        target[productID] = append(target[productID], device.DeviceID)
        remaining--
    }
}

// Only use loose devices if needed (lines 887-902)
if remaining > 0 {
    for _, device := range loose {
        ...
    }
}
```

### 5. API Endpoint for Product Tree with Availability
**Status:** ✅ Implemented

**Endpoint:** `GET /api/v1/devices/tree/availability`

**Parameters:**
- `start_date` (required): YYYY-MM-DD format
- `end_date` (required): YYYY-MM-DD format
- `job_id` (optional): Exclude devices from this job ID

**Response Structure:**
```json
{
  "treeData": [
    {
      "id": 1,
      "name": "Category Name",
      "device_count": 50,
      "available_count": 35,
      "products": [
        {
          "id": 101,
          "name": "Product Name",
          "device_count": 12,
          "available_count": 8
        }
      ],
      "subcategories": [...]
    }
  ]
}
```

**Implementation Details:**
- **Main Handler:** `internal/handlers/device_handler.go:768-804`
- **Availability Logic:** `device_handler.go:898-938`
- **Product Aggregation:** `device_handler.go:996-1047`
- **Route Registration:** `cmd/server/main.go:1240`

## How to Use This Feature

1. **Navigate to Job Creation/Editing:**
   - Go to `/jobs/new` (Create) or `/jobs/:id/edit` (Edit)

2. **Select Dates:**
   - Choose Start Date and End Date
   - Product tree loads automatically after date selection

3. **Browse Product Tree:**
   - Click on categories to expand
   - Navigate through subcategories
   - View products at the lowest level

4. **Select Products:**
   - Enter desired quantity in the input field
   - See available/total count (e.g., "8 / 12")
   - Validation errors show if quantity > availability

5. **Submit Job:**
   - Smart allocation automatically runs
   - Devices are assigned from cases optimally
   - Case integrity is preserved when possible

## Testing the Feature

To verify the feature is working:

1. Open browser developer console (F12)
2. Go to Network tab
3. Create/edit a job and select dates
4. Look for API call: `GET /api/v1/devices/tree/availability?start_date=...&end_date=...`
5. Check response has `treeData` with categories and products
6. Try selecting different quantities
7. Verify validation messages appear when exceeding availability

## Troubleshooting

If the feature doesn't work:

**Problem:** Product tree doesn't load
- **Check:** Are start and end dates selected?
- **Check:** Browser console for JavaScript errors
- **Check:** Network tab for API call failures

**Problem:** Wrong availability counts
- **Check:** Database has correct device assignments
- **Check:** Job dates don't overlap with existing jobs
- **Check:** Devices have proper `productID` assignments

**Problem:** Case allocation not optimal
- **Check:** Devices have `caseID` set in database
- **Check:** Cases contain multiple devices of same product
- **Check:** Allocation logic logs (enable `jobDebugLogsEnabled`)

## Conclusion

**All features from Issue #23 are already fully operational in the codebase.**

The implementation includes:
- Complete product tree UI
- Availability checking with real-time validation
- Smart case-aware device allocation
- Full API support with proper data aggregation

No additional development is required. The feature works as described in the issue.

## Related Files

- `web/templates/job_form.html` - Frontend UI
- `internal/handlers/job_handler.go` - Job creation & device allocation
- `internal/handlers/device_handler.go` - Product tree API & availability
- `cmd/server/main.go` - Route definitions
- `internal/repository/device_repository.go` - Database queries

## Issue Created Date
2025-11-02

## Analysis Date
2025-11-03

---

**Issue can be closed as "Already Implemented"**
