package lastheard

import (
	"net/http"
	"strconv"

	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GETLastheard(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	session := sessions.Default(c)
	userId := session.Get("user_id")
	var calls []models.Call
	if userId == nil {
		// This is okay, we just query the latest X calls
		calls = models.FindCalls(db, 10)
	} else {
		// Get the last calls for the user
		calls = models.FindUserCalls(db, userId.(uint), 10)
	}
	if len(calls) == 0 {
		c.JSON(http.StatusOK, make([]string, 0))
	} else {
		c.JSON(http.StatusOK, calls)
	}
}

func GETLastheardUser(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	userID64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID"})
		return
	}
	userID := uint(userID64)
	c.JSON(http.StatusOK, models.FindUserCalls(db, userID, 10))
}

func GETLastheardRepeater(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	repeaterID64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Repeater ID"})
		return
	}
	repeaterID := uint(repeaterID64)
	c.JSON(http.StatusOK, models.FindRepeaterCalls(db, repeaterID, 10))
}

func GETLastheardTalkgroup(c *gin.Context) {
	db := c.MustGet("DB").(*gorm.DB)
	id := c.Param("id")
	talkgroupID64, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Talkgroup ID"})
		return
	}
	talkgroupID := uint(talkgroupID64)
	c.JSON(http.StatusOK, models.FindTalkgroupCalls(db, talkgroupID, 10))
}
