package middleware

import (
	"fmt"
	"net/http"

	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func RequireAdminOrTGOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireAdminOrTGOwner"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}

		valid := false
		// Open up the DB and check if the user is an admin
		db := c.MustGet("DB").(*gorm.DB).WithContext(ctx)
		var user models.User
		db.Find(&user, "id = ?", userId.(uint))
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && user.Approved && !user.Suspended {
			valid = true
		}

		// Check if the user is the owner of any talkgroups
		talkgroups, err := models.FindTalkgroupsByOwnerID(db, userId.(uint))
		if err != nil {
			klog.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		if len(talkgroups) > 0 && user.Approved && !user.Suspended {
			valid = true
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireAdmin"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}

		valid := false
		// Open up the DB and check if the user is an admin
		db := c.MustGet("DB").(*gorm.DB).WithContext(ctx)
		var user models.User
		db.Find(&user, "id = ?", userId.(uint))
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && user.Approved && !user.Suspended {
			valid = true
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		session := sessions.Default(c)
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireAdmin"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}
		if userId.(uint) != 999999 {
			klog.Error("User is not a super admin")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}

func RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireLogin"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}

		valid := false
		// Open up the DB and check if the user exists
		db := c.MustGet("DB").(*gorm.DB).WithContext(ctx)
		var user models.User
		db.Find(&user, "id = ?", userId.(uint))
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Approved && !user.Suspended {
			valid = true
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
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireRepeaterOwnerOrAdmin"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}

		valid := false
		db := c.MustGet("DB").(*gorm.DB).WithContext(ctx)
		// Open up the DB and check if the user is an admin or if they own repeater with id = id
		var user models.User
		db.Find(&user, "id = ?", userId.(uint))
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Approved && !user.Suspended && user.Admin {
			valid = true
		} else {
			var repeater models.Repeater
			db.Find(&repeater, "radio_id = ?", id)
			if repeater.OwnerID == user.ID && !user.Suspended && user.Approved {
				valid = true
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
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireTalkgroupOwnerOrAdmin"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}

		valid := false
		db := c.MustGet("DB").(*gorm.DB).WithContext(ctx)
		// Open up the DB and check if the user is an admin or if they own talkgroup with id = id
		var user models.User
		db.Find(&user, "id = ?", userId.(uint))
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && !user.Suspended && user.Approved {
			valid = true
		} else {
			var talkgroup models.Talkgroup
			db.Preload("Admins").Find(&talkgroup, "id = ?", id)
			for _, admin := range talkgroup.Admins {
				if admin.ID == user.ID && !user.Suspended && user.Approved {
					valid = true
					break
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
		userId := session.Get("user_id")
		if userId == nil {
			klog.Error("userId not found")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			return
		}
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.auth", "RequireSelfOrAdmin"),
				attribute.Int("user.id", int(userId.(uint))),
			)
		}

		valid := false

		db := c.MustGet("DB").(*gorm.DB).WithContext(ctx)
		// Open up the DB and check if the user is an admin or if their ID matches id
		var user models.User
		db.Find(&user, "id = ?", userId.(uint))
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("user.admin", user.Admin),
			)
		}
		if user.Admin && !user.Suspended && user.Approved {
			valid = true
		} else {
			if id == fmt.Sprintf("%d", user.ID) && !user.Suspended && user.Approved {
				valid = true
			}
		}

		if !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
		}
	}
}
