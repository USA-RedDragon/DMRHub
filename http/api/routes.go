package api

import (
	v1Controllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1"
	"github.com/gin-gonic/gin"
)

// ApplyRoutes to the HTTP Mux
func ApplyRoutes(router *gin.Engine, redisHost string) {
	apiV1 := router.Group("/api/v1")
	v1(apiV1)
}

func v1(group *gin.RouterGroup) {
	group.GET("/version", v1Controllers.GETVersion)
	group.GET("/ping", v1Controllers.GETPing)
}
