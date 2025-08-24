package setupwizard

import (
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GETSetupWizard(c *gin.Context) {
	c.JSON(200, gin.H{"setupwizard": true})
}

func POSTSetupWizardComplete(c *gin.Context) {
	if v, ok := c.MustGet("SetupWizardConfigCompleteChan").(chan any); ok {
		// Notify the setup wizard that config is complete
		v <- struct{}{}
	} else {
		slog.Warn("SetupWizardConfigCompleteChan not found in context or of wrong type")
	}

	c.JSON(200, gin.H{"message": "Setup wizard marked as complete"})
}

func ApproveAndPromoteAdminUser(c *gin.Context) {
	newUserID, ok := c.MustGet("new_user_id").(uint)
	if !ok {
		slog.Error("ApproveAndPromoteAdminUser: new_user_id not found in context or of wrong type")
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}
	if db, ok := c.MustGet("DB").(*gorm.DB); ok {
		if user, err := models.FindUserByID(db, newUserID); err == nil {
			// Promote the user to admin
			user.Approved = true
			user.Admin = true
			user.SuperAdmin = true
			if err := db.Save(user).Error; err != nil {
				slog.Error("Failed to promote user to admin", "error", err)
				c.JSON(500, gin.H{"error": "Internal server error"})
				return
			}
		} else {
			slog.Error("Failed to find user by ID", "error", err)
			c.JSON(500, gin.H{"error": "Internal server error"})
			return
		}
	} else {
		slog.Error("DB not found in context or of wrong type")
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}
}
