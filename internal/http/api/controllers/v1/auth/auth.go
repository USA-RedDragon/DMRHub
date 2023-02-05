package auth

import (
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func POSTLogin(c *gin.Context) {
	session := sessions.Default(c)
	db := c.MustGet("DB").(*gorm.DB)

	var json apimodels.AuthLogin
	err := c.ShouldBindJSON(&json)
	if err != nil {
		klog.Errorf("POSTLogin: JSON data is invalid: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON data is invalid"})
	} else {
		// Check that one of username or callsign is not blank
		if json.Username == "" && json.Callsign == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username or Callsign must be provided"})
			return
		}
		// Check that password isn't a zero string
		if json.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password cannot be blank"})
			return
		}
		var user models.User
		if json.Username != "" {
			db.Find(&user, "username = ?", json.Username)
		} else {
			db.Find(&user, "callsign = ?", json.Callsign)
		}
		if user.ID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"status": 401, "error": "Authentication failed"})
			return
		}
		verified, err := utils.VerifyPassword(json.Password, user.Password, config.GetConfig().PasswordSalt)
		klog.Infof("POSTLogin: Password verified %v", verified)
		if verified && err == nil {
			if user.Approved {
				session.Set("user_id", user.ID)
				err = session.Save()
				if err != nil {
					klog.Errorf("POSTLogin: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{"status": 500, "error": "Error saving session"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"status": 200, "message": "Logged in"})
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"status": 401, "error": "User is not approved"})
			return
		}
		klog.Errorf("POSTLogin: %v", err)
	}

	c.JSON(http.StatusUnauthorized, gin.H{"status": 401, "error": "Authentication failed"})
}

func GETLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	err := session.Save()
	if err != nil {
		klog.Errorf("GETLogout: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"status": 500, "error": "Error saving session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "Logged out"})
}
