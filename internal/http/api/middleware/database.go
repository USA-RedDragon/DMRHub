package middleware

import (
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func DatabaseProvider(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.GetConfig().OTLPEndpoint != "" {
			c.Set("DB", db.WithContext(c.Request.Context()))
		} else {
			c.Set("DB", db)
		}
		c.Next()
	}
}
