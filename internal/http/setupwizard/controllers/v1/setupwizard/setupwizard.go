package setupwizard

import "github.com/gin-gonic/gin"

func GETSetupWizard(c *gin.Context) {
	c.JSON(200, gin.H{"setupwizard": true})
}
