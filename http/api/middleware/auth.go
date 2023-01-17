package middleware

import (
	"fmt"
	"net/http"

	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		valid := false
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		} else {
			// Open up the DB and check if the user is an admin
			db := c.MustGet("DB").(*gorm.DB)
			var user models.User
			db.Find(&user, "id = ?", userId)
			if user.Admin {
				valid = true
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		valid := false
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		} else {
			// Open up the DB and check if the user exists
			db := c.MustGet("DB").(*gorm.DB)
			var user models.User
			db.Find(&user, "id = ?", userId)
			if user.Approved {
				valid = true
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireRepeaterOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")
		valid := false
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		} else {
			db := c.MustGet("DB").(*gorm.DB)
			// Open up the DB and check if the user is an admin or if they own repeater with id = id
			var user models.User
			db.Find(&user, "id = ?", userId)
			if user.Admin {
				valid = true
			} else {
				var repeater models.Repeater
				db.Find(&repeater, "radio_id = ?", id)
				if repeater.OwnerID == user.ID {
					valid = true
				}
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireTalkgroupOwnerOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")
		valid := false
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		} else {
			db := c.MustGet("DB").(*gorm.DB)
			// Open up the DB and check if the user is an admin or if they own talkgroup with id = id
			var user models.User
			db.Find(&user, "id = ?", userId)
			if user.Admin {
				valid = true
			} else {
				var talkgroup models.Talkgroup
				db.Preload("Admins").Find(&talkgroup, "id = ?", id)
				for _, admin := range talkgroup.Admins {
					if admin.ID == user.ID {
						valid = true
						break
					}
				}
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireSelfOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := c.Param("id")
		valid := false
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		} else {
			db := c.MustGet("DB").(*gorm.DB)
			// Open up the DB and check if the user is an admin or if their ID matches id
			var user models.User
			db.Find(&user, "id = ?", userId)
			if user.Admin {
				valid = true
			} else {
				if id == fmt.Sprintf("%d", user.ID) {
					valid = true
				}
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}
