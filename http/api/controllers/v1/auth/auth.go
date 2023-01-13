package auth

import (
	"net/http"

	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/apimodels"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/utils"
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
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
			c.JSON(http.StatusUnauthorized, gin.H{"status": 401, "message": "1 Authentication failed"})
			return
		}
		verified, err := utils.VerifyPassword(json.Password, user.Password)
		klog.Infof("POSTLogin: Password verified %v", verified)
		if verified && err == nil {
			if user.Approved {
				session.Set("user_id", user.ID)
				session.Set("admin", user.Admin)
				session.Save()
				c.JSON(http.StatusOK, gin.H{"status": 200, "message": "Logged in"})
				return
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"status": 401, "message": "User is not approved"})
				return
			}
		} else {
			klog.Errorf("POSTLogin: %v", err)
		}
	}

	c.JSON(http.StatusUnauthorized, gin.H{"status": 401, "message": "2 Authentication failed"})
}

func GETLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "Logged out"})
}
