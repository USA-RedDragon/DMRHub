package api

import (
	v1Controllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1"
	v1AuthControllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1/auth"
	v1LastheardControllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1/lastheard"
	v1RepeatersControllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1/repeaters"
	v1TalkgroupsControllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1/talkgroups"
	v1UsersControllers "github.com/USA-RedDragon/dmrserver-in-a-box/http/api/controllers/v1/users"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/middleware"
	"github.com/gin-gonic/gin"
)

// ApplyRoutes to the HTTP Mux
func ApplyRoutes(router *gin.Engine, redisHost string) {
	apiV1 := router.Group("/api/v1")
	v1(apiV1)
}

func v1(group *gin.RouterGroup) {
	v1Auth := group.Group("/auth")
	v1Auth.POST("/login", v1AuthControllers.POSTLogin)
	v1Auth.GET("/logout", v1AuthControllers.GETLogout)

	v1Repeaters := group.Group("/repeaters")
	v1Repeaters.GET("", middleware.RequireAdmin(), v1RepeatersControllers.GETRepeaters)
	v1Repeaters.GET("/my", middleware.RequireLogin(), v1RepeatersControllers.GETMyRepeaters)
	v1Repeaters.POST("", middleware.RequireLogin(), v1RepeatersControllers.POSTRepeater)
	v1Repeaters.POST("/:id/link/:type/:slot/:target", middleware.RequireRepeaterOwnerOrAdmin(), v1RepeatersControllers.POSTRepeaterLink)
	v1Repeaters.POST("/:id/unlink/:type/:slot/:target", middleware.RequireRepeaterOwnerOrAdmin(), v1RepeatersControllers.POSTRepeaterUnink)
	v1Repeaters.GET("/:id", middleware.RequireLogin(), v1RepeatersControllers.GETRepeater)
	v1Repeaters.PATCH("/:id", middleware.RequireRepeaterOwnerOrAdmin(), v1RepeatersControllers.PATCHRepeater)
	v1Repeaters.DELETE("/:id", middleware.RequireRepeaterOwnerOrAdmin(), v1RepeatersControllers.DELETERepeater)

	v1Talkgroups := group.Group("/talkgroups")
	v1Talkgroups.GET("", middleware.RequireLogin(), v1TalkgroupsControllers.GETTalkgroups)
	v1Talkgroups.GET("/my", middleware.RequireLogin(), v1TalkgroupsControllers.GETMyTalkgroups)
	v1Talkgroups.POST("", middleware.RequireAdmin(), v1TalkgroupsControllers.POSTTalkgroup)
	v1Talkgroups.POST("/:id/appoint", middleware.RequireAdmin(), v1TalkgroupsControllers.POSTTalkgroupAdminAppoint)
	v1Talkgroups.POST("/:id/demote", middleware.RequireAdmin(), v1TalkgroupsControllers.POSTTalkgroupAdminDemote)
	v1Talkgroups.GET("/:id", middleware.RequireLogin(), v1TalkgroupsControllers.GETTalkgroup)
	v1Talkgroups.PATCH("/:id", middleware.RequireTalkgroupOwnerOrAdmin(), v1TalkgroupsControllers.PATCHTalkgroup)
	v1Talkgroups.DELETE("/:id", middleware.RequireAdmin(), v1TalkgroupsControllers.DELETETalkgroup)

	v1Users := group.Group("/users")
	v1Users.GET("", middleware.RequireAdmin(), v1UsersControllers.GETUsers)
	v1Users.POST("", v1UsersControllers.POSTUser)
	v1Users.GET("/me", middleware.RequireLogin(), v1UsersControllers.GETUserSelf)
	v1Users.GET("/admins", middleware.RequireAdmin(), v1UsersControllers.GETUserAdmins)
	v1Users.POST("/promote/:id", middleware.RequireAdmin(), v1UsersControllers.POSTUserPromote)
	v1Users.POST("/demote/:id", middleware.RequireAdmin(), v1UsersControllers.POSTUserDemote)
	v1Users.POST("/approve/:id", middleware.RequireAdmin(), v1UsersControllers.POSTUserApprove)
	v1Users.POST("/suspend/:id", middleware.RequireAdmin(), v1UsersControllers.POSTUserSuspend)
	v1Users.GET("/:id", middleware.RequireSelfOrAdmin(), v1UsersControllers.GETUser)
	v1Users.PATCH("/:id", middleware.RequireSelfOrAdmin(), v1UsersControllers.PATCHUser)
	v1Users.DELETE("/:id", middleware.RequireAdmin(), v1UsersControllers.DELETEUser)

	v1Lastheard := group.Group("/lastheard")
	// Returns the lastheard data for the server, adds personal data if logged in
	v1Lastheard.GET("", v1LastheardControllers.GETLastheard)
	// Returns the lastheard data for a given user
	v1Lastheard.GET("/user/:id", middleware.RequireSelfOrAdmin(), v1LastheardControllers.GETLastheardUser)
	// Returns the lastheard data for a given repeater
	v1Lastheard.GET("/repeater/:id", middleware.RequireRepeaterOwnerOrAdmin(), v1LastheardControllers.GETLastheardRepeater)
	// Returns the lastheard data for a given talkgroup
	v1Lastheard.GET("/talkgroup/:id", middleware.RequireLogin(), v1LastheardControllers.GETLastheardTalkgroup)

	group.GET("/version", v1Controllers.GETVersion)
	group.GET("/ping", v1Controllers.GETPing)
}
