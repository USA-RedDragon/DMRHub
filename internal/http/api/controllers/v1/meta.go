package v1

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/sdk"
	"github.com/gin-gonic/gin"
)

func GETVersion(c *gin.Context) {
	_, err := io.WriteString(c.Writer, fmt.Sprintf("%s-%s", sdk.Version, sdk.GitCommit))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting version"})
	}
}

func GETPing(c *gin.Context) {
	_, err := io.WriteString(c.Writer, fmt.Sprintf("%d", time.Now().Unix()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error getting ping"})
	}
}
