package middleware

import (
	"os"

	"github.com/gin-gonic/gin"
)

// TemplateGlobalsMiddleware injects global template variables
func TemplateGlobalsMiddleware() gin.HandlerFunc {
	// Read domain configuration from environment variables
	// These should be just the domain (e.g., "storage.server-nt.de")
	// without protocol or port - the frontend will add the protocol
	storageCoreDomain := os.Getenv("STORAGECORE_DOMAIN")
	rentalCoreDomain := os.Getenv("RENTALCORE_DOMAIN")

	return func(c *gin.Context) {
		// Set template globals in context
		c.Set("StorageCoreDomain", storageCoreDomain)
		c.Set("RentalCoreDomain", rentalCoreDomain)
		c.Next()
	}
}
