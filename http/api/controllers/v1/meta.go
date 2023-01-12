package v1

import (
	"fmt"
	"io"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
	"github.com/gin-gonic/gin"
)

func GETVersion(c *gin.Context) {
	io.WriteString(c.Writer, fmt.Sprintf("%s-%s", sdk.Version, sdk.GitCommit))
}

func GETPing(c *gin.Context) {
	io.WriteString(c.Writer, fmt.Sprintf("%d", time.Now().Unix()))
}
