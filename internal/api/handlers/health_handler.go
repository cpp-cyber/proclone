package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PUBLIC: HealthCheckHandler handles GET requests for health checks with detailed service status
func HealthCheckHandler(authHandler *AuthHandler, cloningHandler *CloningHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		healthStatus := gin.H{
			"status": "healthy",
			"services": gin.H{
				"api": "healthy",
			},
		}

		statusCode := http.StatusOK

		// Check LDAP connection
		if authHandler != nil && authHandler.authService != nil {
			if err := authHandler.authService.HealthCheck(); err != nil {
				healthStatus["services"].(gin.H)["ldap"] = gin.H{
					"status": "unhealthy",
					"error":  err.Error(),
				}
				healthStatus["status"] = "degraded"
				statusCode = http.StatusServiceUnavailable
			} else {
				healthStatus["services"].(gin.H)["ldap"] = "healthy"
			}
		}

		// Check database connection (via cloning handler)
		if cloningHandler != nil {
			if err := cloningHandler.HealthCheck(); err != nil {
				healthStatus["services"].(gin.H)["database"] = gin.H{
					"status": "unhealthy",
					"error":  err.Error(),
				}
				healthStatus["status"] = "degraded"
				statusCode = http.StatusServiceUnavailable
			} else {
				healthStatus["services"].(gin.H)["database"] = "healthy"
			}
		}

		c.JSON(statusCode, healthStatus)
	}
}
