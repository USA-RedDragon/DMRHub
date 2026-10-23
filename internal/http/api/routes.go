// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package api

import (
	"net/http"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	v1Controllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1"
	v1AuthControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/auth"
	v1ConfigControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/config"
	v1LastheardControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/lastheard"
	v1MessagesControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/messages"
	v1NetsControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/nets"
	v1PeersControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/peers"
	v1RepeatersControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/repeaters"
	v1TalkgroupsControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/talkgroups"
	v1UserDBControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/userdb"
	v1UsersControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/users"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	websocketControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ApplyRoutes to the HTTP Mux.
func ApplyRoutes(config *configPkg.Config, router *gin.Engine, dmrHub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, ratelimit gin.HandlerFunc, userSuspension gin.HandlerFunc) {
	router.GET("/robots.txt", func(c *gin.Context) {
		switch config.HTTP.RobotsTXT.Mode {
		case configPkg.RobotsTXTModeDisabled:
			c.String(http.StatusOK, "User-agent: *\nDisallow: /")
		case configPkg.RobotsTXTModeAllow:
			c.String(http.StatusOK, "User-agent: *\nAllow: /\nDisallow: /admin\nSitemap: /sitemap.xml")
		case configPkg.RobotsTXTModeCustom:
			c.String(http.StatusOK, config.HTTP.RobotsTXT.Content+"\nSitemap: /sitemap.xml\nDisallow: /admin")
		}
	})

	apiV1 := router.Group("/api/v1")
	apiV1.Use(ratelimit)
	v1(config, apiV1, userSuspension)

	ws := router.Group("/ws")
	ws.Use(ratelimit, websocket.WatchUser(pubsub))
	ws.GET("/repeaters", middleware.RequireLogin(), userSuspension, websocket.CreateHandler(config, websocketControllers.CreateRepeatersWebsocket(db, pubsub)))
	ws.GET("/calls", middleware.RequireLogin(), userSuspension, websocket.CreateHandler(config, websocketControllers.CreateCallsWebsocket(dmrHub, db, pubsub)))
	ws.GET("/peers", requireOpenBridge(config), middleware.RequireLogin(), userSuspension, websocket.CreateHandler(config, websocketControllers.CreatePeersWebsocket(db, pubsub)))
	ws.GET("/messages", middleware.RequireLogin(), userSuspension, websocket.CreateHandler(config, websocketControllers.CreateMessagesWebsocket(pubsub)))
	ws.GET("/nets", middleware.RequireLogin(), userSuspension, websocket.CreateHandler(config, websocketControllers.CreateNetsWebsocket(pubsub)))
}

// requireOpenBridge answers 404 while OpenBridge is off, as for a route
// that does not exist. It reads the config at each request, so turning
// OpenBridge on or off in the Setup page takes effect at once.
func requireOpenBridge(config *configPkg.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.DMR.OpenBridge.Enabled {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}

func v1(config *configPkg.Config, group *gin.RouterGroup, userSuspension gin.HandlerFunc) {
	v1Auth := group.Group("/auth")
	v1Auth.POST("/login", v1AuthControllers.POSTLogin)
	v1Auth.POST("/logout", v1AuthControllers.POSTLogout)

	v1Repeaters := group.Group("/repeaters")
	// Paginated
	v1Repeaters.GET("", middleware.RequireAdmin(), userSuspension, v1RepeatersControllers.GETRepeaters)
	// Paginated
	v1Repeaters.GET("/my", middleware.RequireLogin(), userSuspension, v1RepeatersControllers.GETMyRepeaters)
	v1Repeaters.POST("", middleware.RequireLogin(), userSuspension, v1RepeatersControllers.POSTRepeater)
	v1Repeaters.POST("/:id/link/:type/:slot/:target", middleware.RequireRepeaterOwnerOrAdmin(), userSuspension, v1RepeatersControllers.POSTRepeaterLink)
	v1Repeaters.POST("/:id/unlink/:type/:slot/:target", middleware.RequireRepeaterOwnerOrAdmin(), userSuspension, v1RepeatersControllers.POSTRepeaterUnlink)
	v1Repeaters.POST("/:id/talkgroups", middleware.RequireRepeaterOwnerOrAdmin(), userSuspension, v1RepeatersControllers.POSTRepeaterTalkgroups)
	v1Repeaters.GET("/:id", middleware.RequireLogin(), userSuspension, v1RepeatersControllers.GETRepeater)
	v1Repeaters.PATCH("/:id", middleware.RequireRepeaterOwnerOrAdmin(), userSuspension, v1RepeatersControllers.PATCHRepeater)
	v1Repeaters.DELETE("/:id", middleware.RequireRepeaterOwnerOrAdmin(), userSuspension, v1RepeatersControllers.DELETERepeater)

	v1Talkgroups := group.Group("/talkgroups")
	// Paginated
	v1Talkgroups.GET("", v1TalkgroupsControllers.GETTalkgroups)
	// Paginated
	v1Talkgroups.GET("/my", middleware.RequireLogin(), userSuspension, v1TalkgroupsControllers.GETMyTalkgroups)
	v1Talkgroups.POST("", middleware.RequireAdmin(), userSuspension, v1TalkgroupsControllers.POSTTalkgroup)
	v1Talkgroups.POST("/:id/admins", middleware.RequireAdmin(), userSuspension, v1TalkgroupsControllers.POSTTalkgroupAdmins)
	v1Talkgroups.POST("/:id/ncos", middleware.RequireTalkgroupOwnerOrAdmin(), userSuspension, v1TalkgroupsControllers.POSTTalkgroupNCOs)
	v1Talkgroups.GET("/:id", v1TalkgroupsControllers.GETTalkgroup)
	v1Talkgroups.PATCH("/:id", middleware.RequireTalkgroupOwnerOrAdmin(), userSuspension, v1TalkgroupsControllers.PATCHTalkgroup)
	v1Talkgroups.DELETE("/:id", middleware.RequireAdmin(), userSuspension, v1TalkgroupsControllers.DELETETalkgroup)

	v1Users := group.Group("/users")
	// Paginated
	v1Users.GET("", middleware.RequireAdminOrTGOwner(), userSuspension, v1UsersControllers.GETUsers)
	v1Users.POST("", v1UsersControllers.POSTUser)
	v1Users.GET("/me", middleware.RequireLogin(), userSuspension, v1UsersControllers.GETUserSelf)
	v1Users.GET("/me/settings", middleware.RequireLogin(), userSuspension, v1UsersControllers.GETUserSettings)
	v1Users.PUT("/me/settings/sms-storage", middleware.RequireLogin(), userSuspension, v1UsersControllers.PUTUserSMSStorage)
	v1Users.GET("/me/radio-ids", middleware.RequireLogin(), userSuspension, v1UsersControllers.GETMyRadioIDs)
	v1Users.POST("/me/radio-ids", middleware.RequireLogin(), userSuspension, v1UsersControllers.POSTMyRadioID)
	v1Users.DELETE("/me/radio-ids/:radio_id", middleware.RequireLogin(), userSuspension, v1UsersControllers.DELETEMyRadioID)
	// Paginated
	v1Users.GET("/admins", middleware.RequireSuperAdmin(), userSuspension, v1UsersControllers.GETUserAdmins)
	// Paginated
	v1Users.GET("/suspended", middleware.RequireAdmin(), userSuspension, v1UsersControllers.GETUserSuspended)
	v1Users.GET("/unapproved", middleware.RequireAdmin(), userSuspension, v1UsersControllers.GETUserUnapproved)
	v1Users.POST("/promote/:id", middleware.RequireSuperAdmin(), userSuspension, v1UsersControllers.POSTUserPromote)
	v1Users.POST("/demote/:id", middleware.RequireSuperAdmin(), userSuspension, v1UsersControllers.POSTUserDemote)
	v1Users.POST("/approve/:id", middleware.RequireAdmin(), userSuspension, v1UsersControllers.POSTUserApprove)
	v1Users.POST("/unsuspend/:id", middleware.RequireAdmin(), userSuspension, v1UsersControllers.POSTUserUnsuspend)
	v1Users.POST("/suspend/:id", middleware.RequireAdmin(), userSuspension, v1UsersControllers.POSTUserSuspend)
	v1Users.GET("/:id/profile", middleware.RequireLogin(), userSuspension, v1UsersControllers.GETUserProfile)
	v1Users.GET("/:id/radio-ids", middleware.RequireAdmin(), userSuspension, v1UsersControllers.GETUserRadioIDs)
	v1Users.POST("/:id/radio-ids", middleware.RequireAdmin(), userSuspension, v1UsersControllers.POSTUserRadioID)
	v1Users.DELETE("/:id/radio-ids/:radio_id", middleware.RequireAdmin(), userSuspension, v1UsersControllers.DELETEUserRadioID)
	v1Users.GET("/:id", middleware.RequireSelfOrAdmin(), userSuspension, v1UsersControllers.GETUser)
	v1Users.PATCH("/:id", middleware.RequireSelfOrAdmin(), userSuspension, v1UsersControllers.PATCHUser)
	v1Users.DELETE("/:id", middleware.RequireSuperAdmin(), userSuspension, v1UsersControllers.DELETEUser)

	{
		v1Peers := group.Group("/peers")
		v1Peers.Use(requireOpenBridge(config))
		// Paginated
		v1Peers.GET("", middleware.RequireAdmin(), v1PeersControllers.GETPeers)
		// Paginated
		v1Peers.GET("/my", middleware.RequireLogin(), v1PeersControllers.GETMyPeers)
		v1Peers.POST("", middleware.RequireAdmin(), v1PeersControllers.POSTPeer)
		v1Peers.GET("/:id", middleware.RequirePeerOwnerOrAdmin(), v1PeersControllers.GETPeer)
		v1Peers.PATCH("/:id", middleware.RequirePeerOwnerOrAdmin(), v1PeersControllers.PATCHPeer)
		v1Peers.DELETE("/:id", middleware.RequirePeerOwnerOrAdmin(), v1PeersControllers.DELETEPeer)
		v1Peers.GET("/:id/rules", middleware.RequirePeerOwnerOrAdmin(), v1PeersControllers.GETPeerRules)
		v1Peers.POST("/:id/rules", middleware.RequirePeerOwnerOrAdmin(), v1PeersControllers.POSTPeerRule)
		v1Peers.DELETE("/:id/rules/:ruleId", middleware.RequirePeerOwnerOrAdmin(), v1PeersControllers.DELETEPeerRule)
	}

	v1Messages := group.Group("/messages")
	v1Messages.Use(middleware.RequireLogin(), userSuspension)
	v1Messages.GET("/inbox", v1MessagesControllers.GETInbox)
	v1Messages.GET("/outbox", v1MessagesControllers.GETOutbox)
	v1Messages.GET("/stats", v1MessagesControllers.GETStats)
	v1Messages.GET("/:id", v1MessagesControllers.GETMessage)

	v1Admin := group.Group("/admin")
	v1Admin.Use(middleware.RequireAdmin(), userSuspension)
	v1Admin.GET("/messages", v1MessagesControllers.GETAdminMessages)
	v1Admin.GET("/messages/:id", v1MessagesControllers.GETAdminMessage)

	v1Lastheard := group.Group("/lastheard")
	// Returns the lastheard data for the server, adds personal data if logged in
	// Paginated
	v1Lastheard.GET("", v1LastheardControllers.GETLastheard)
	// Paginated
	v1Lastheard.GET("/user/:id", middleware.RequireSelfOrAdmin(), userSuspension, v1LastheardControllers.GETLastheardUser)
	// Paginated
	v1Lastheard.GET("/repeater/:id", middleware.RequireRepeaterOwnerOrAdmin(), userSuspension, v1LastheardControllers.GETLastheardRepeater)
	// Paginated
	v1Lastheard.GET("/talkgroup/:id", v1LastheardControllers.GETLastheardTalkgroup)

	v1Config := group.Group("/config")
	v1Config.Use(middleware.RequireAdmin(), userSuspension)
	v1Config.PUT("", v1ConfigControllers.PUTConfig)
	v1Config.GET("", v1ConfigControllers.GETConfig)
	v1Config.GET("/validate", v1ConfigControllers.GETConfigValidate)
	v1Config.POST("/validate", v1ConfigControllers.POSTConfigValidate)

	v1Nets := group.Group("/nets")
	// Paginated
	v1Nets.GET("", v1NetsControllers.GETNets)
	v1Nets.GET("/:id", v1NetsControllers.GETNet)
	v1Nets.POST("/start", middleware.RequireLogin(), userSuspension, v1NetsControllers.POSTNetStart)
	v1Nets.POST("/:id/stop", middleware.RequireNetNCOOrOwnerOrAdmin(), userSuspension, v1NetsControllers.POSTNetStop)
	v1Nets.PATCH("/:id", middleware.RequireAdmin(), userSuspension, v1NetsControllers.PATCHNet)
	// Paginated
	v1Nets.GET("/:id/checkins", v1NetsControllers.GETNetCheckIns)
	v1Nets.GET("/:id/checkins/export", middleware.RequireLogin(), userSuspension, v1NetsControllers.GETNetCheckInsExport)

	v1ScheduledNets := v1Nets.Group("/scheduled")
	// Paginated
	v1ScheduledNets.GET("", v1NetsControllers.GETScheduledNets)
	v1ScheduledNets.GET("/:id", v1NetsControllers.GETScheduledNet)
	v1ScheduledNets.POST("", middleware.RequireLogin(), userSuspension, v1NetsControllers.POSTScheduledNet)
	v1ScheduledNets.PATCH("/:id", middleware.RequireScheduledNetNCOOrOwnerOrAdmin(), userSuspension, v1NetsControllers.PATCHScheduledNet)
	v1ScheduledNets.DELETE("/:id", middleware.RequireScheduledNetNCOOrOwnerOrAdmin(), userSuspension, v1NetsControllers.DELETEScheduledNet)

	v1UserDB := group.Group("/userdb")
	v1UserDB.GET("/:id", v1UserDBControllers.GETUserDBEntry)

	group.GET("/network/name", v1Controllers.GETNetworkName)
	group.GET("/network/features", v1Controllers.GETNetworkFeatures)
	group.GET("/network/webtransport", v1Controllers.GETNetworkWebTransport)
	group.GET("/version", v1Controllers.GETVersion)
	group.GET("/ping", v1Controllers.GETPing)
	group.GET("/healthcheck", v1Controllers.GETHealthcheck)
}
