package lastheard

import (
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GETLastheard(c *gin.Context) {
	db := c.MustGet("PaginatedDB").(*gorm.DB)
	cDb := c.MustGet("DB").(*gorm.DB)
	session := sessions.Default(c)
	userID := session.Get("user_id")
	var calls []models.Call
	var count int
	if userID == nil {
		// This is okay, we just query the latest public calls
		calls = models.FindCalls(db)
		count = models.CountCalls(cDb)
	} else {
		// Get the last calls for the user
		calls = models.FindUserCalls(db, userID.(uint))
		count = models.CountUserCalls(cDb, userID.(uint))
	}
	if len(calls) == 0 {
		c.JSON(http.StatusOK, make([]string, 0))
	} else {
		c.JSON(http.StatusOK, gin.H{"calls": calls, "total": count})
	}
}

func GETLastheardUser(c *gin.Context) {
	db := c.MustGet("PaginatedDB").(*gorm.DB)
	cDb := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	userID64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID"})
		return
	}
	userID := uint(userID64)
	calls := models.FindUserCalls(db, userID)
	count := models.CountUserCalls(cDb, userID)
	c.JSON(http.StatusOK, gin.H{"calls": calls, "total": count})
}

func GETLastheardRepeater(c *gin.Context) {
	db := c.MustGet("PaginatedDB").(*gorm.DB)
	cDb := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	repeaterID64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	repeaterID := uint(repeaterID64)
	calls := models.FindRepeaterCalls(db, repeaterID)
	count := models.CountRepeaterCalls(cDb, repeaterID)
	c.JSON(http.StatusOK, gin.H{"calls": calls, "total": count})
}

func GETLastheardTalkgroup(c *gin.Context) {
	db := c.MustGet("PaginatedDB").(*gorm.DB)
	id := c.Param("id")
	talkgroupID64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Talkgroup ID"})
		return
	}
	talkgroupID := uint(talkgroupID64)
	calls := models.FindTalkgroupCalls(db, talkgroupID)
	count := models.CountTalkgroupCalls(db, talkgroupID)
	c.JSON(http.StatusOK, gin.H{"calls": calls, "total": count})
}
