package middleware

import (
	"os"

	"github.com/gin-gonic/gin"
)

// TemplateGlobalsMiddleware injects global template variables into the request context.
// These can be retrieved in handlers with c.Get("key") and then passed to templates,
// or accessed via template functions registered in the funcMap (e.g. currencySymbol()).
func TemplateGlobalsMiddleware() gin.HandlerFunc {
	// Read domain configuration from environment variables
	// These should be just the domain (e.g., "storage.server-nt.de")
	// without protocol or port - the frontend will add the protocol
	storageCoreDomain := os.Getenv("WAREHOUSECORE_DOMAIN")
	rentalCoreDomain := os.Getenv("RENTALCORE_DOMAIN")

	return func(c *gin.Context) {
		// Set template globals in context
		c.Set("WarehouseCoreDomain", storageCoreDomain)
		c.Set("RentalCoreDomain", rentalCoreDomain)
		c.Next()
	}
}
