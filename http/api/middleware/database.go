package middleware

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func DatabaseProvider(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		klog.Info("DatabaseProvider: Setting DB in context")
		c.Set("DB", db)
		c.Next()
	}
}
