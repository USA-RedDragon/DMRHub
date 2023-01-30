package middleware

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func DatabaseProvider(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("DB", db.WithContext(c.Request.Context()))
		c.Next()
	}
}
