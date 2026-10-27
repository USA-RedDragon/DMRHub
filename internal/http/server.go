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

package http

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/netscheduler"
	"github.com/USA-RedDragon/DMRHub/internal/http/api"
	v1AuthControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/auth"
	v1ConfigControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/config"
	v1OAuthControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/oauth"
	v1PluginsControllers "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	gormRateLimit "github.com/USA-RedDragon/DMRHub/internal/http/ratelimit"
	"github.com/USA-RedDragon/DMRHub/internal/http/sessionstore"
	"github.com/USA-RedDragon/DMRHub/internal/http/setupwizard"
	v1SetupWizardControllers "github.com/USA-RedDragon/DMRHub/internal/http/setupwizard/controllers/v1/setupwizard"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"
)

var (
	ErrReadDir = errors.New("error reading directory")
)

type Server struct {
	*http.Server
	shutdownChannel chan bool
	stopOnce        sync.Once
}

const defTimeout = 10 * time.Second
const rateLimitRate = time.Second
const rateLimitLimit = 50

// Option changes what CreateRouter puts into the router.
type Option func(*options)

type options struct {
	live         *liveconfig.Live
	hibpClient   *http.Client
	mailer       smtp.Mailer
	pluginEngine *wasm.Engine
	kv           kv.KV
	modemHost    v1ConfigControllers.ModemHost
}

// WithLiveConfig makes the router run with the config of l and save the
// Setup settings with l. Without it, the router saves them with a Live of
// its own that shares them with no other replica.
func WithLiveConfig(l *liveconfig.Live) Option {
	return func(o *options) { o.live = l }
}

// WithHIBPClient makes the password checks query HaveIBeenPwned with
// client.
func WithHIBPClient(client *http.Client) Option {
	return func(o *options) { o.hibpClient = client }
}

// WithPluginEngine makes the plugins API check uploaded plugins with e,
// whose cache then holds them for the plugins inside DMRHub.
func WithPluginEngine(e *wasm.Engine) Option {
	return func(o *options) { o.pluginEngine = e }
}

// WithKV makes the plugins API read from store how the plugins inside
// DMRHub last stopped.
func WithKV(store kv.KV) Option {
	return func(o *options) { o.kv = store }
}

// WithModemHost makes the modem API show the live state of the modem
// server of h.
func WithModemHost(h v1ConfigControllers.ModemHost) Option {
	return func(o *options) { o.modemHost = h }
}

// WithMailer makes the password reset emails go out through m instead of
// the SMTP server of the config.
func WithMailer(m smtp.Mailer) Option {
	return func(o *options) { o.mailer = m }
}

func MakeServer(ctx context.Context, config *configPkg.Config, dmrHub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, ready *atomic.Bool, ns *netscheduler.NetScheduler, version, commit string, opts ...Option) Server {
	r := CreateRouter(ctx, config, dmrHub, db, pubsub, ready, ns, version, commit, opts...)

	slog.Info("HTTP Server listening", "bind", config.HTTP.Bind, "port", config.HTTP.Port)
	s := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.HTTP.Bind, config.HTTP.Port),
		Handler:      r,
		ReadTimeout:  defTimeout,
		WriteTimeout: defTimeout,
	}
	s.SetKeepAlivesEnabled(false)

	return Server{
		Server:          s,
		shutdownChannel: make(chan bool),
	}
}

// MakeSetupWizardServer returns the server of the setup wizard, whose
// phase state holds. It listens on http.bind and http.port of config.
func MakeSetupWizardServer(config *configPkg.Config, state *setupwizard.State, configCompleteChan chan any, version, commit string) Server {
	r := CreateSetupWizardRouter(config, state, configCompleteChan, version, commit)

	slog.Info("HTTP Server listening", "bind", config.HTTP.Bind, "port", config.HTTP.Port)
	s := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.HTTP.Bind, config.HTTP.Port),
		Handler:      r,
		ReadTimeout:  defTimeout,
		WriteTimeout: defTimeout,
	}
	s.SetKeepAlivesEnabled(false)

	return Server{
		Server:          s,
		shutdownChannel: make(chan bool),
	}
}

// FS is the embedded frontend files
//
//go:generate sh -c "cd ./frontend && npm ci && npm run build"
//go:embed all:frontend/dist
var FS embed.FS

func addMiddleware(ctx context.Context, config *configPkg.Config, dmrHub *hub.Hub, r *gin.Engine, db *gorm.DB, pubsub pubsub.PubSub, ready *atomic.Bool, ns *netscheduler.NetScheduler, version, commit string, o options) {
	// Tracing
	if config.Metrics.OTLPEndpoint != "" {
		r.Use(otelgin.Middleware("api"))
		r.Use(middleware.TracingProvider(config))
	}

	// DBs
	r.Use(func(g *gin.Context) { middleware.Provider("DB", db.WithContext(g.Request.Context()))(g) }) //nolint:contextcheck
	r.Use(middleware.PaginatedDatabaseProvider(db, middleware.PaginationConfig{}))

	r.Use(middleware.Provider("PubSub", pubsub))
	r.Use(currentConfig(o.live.Current()))
	r.Use(middleware.Provider(v1ConfigControllers.LiveKey, o.live))
	modemAPI := &v1ConfigControllers.Modem{Host: o.modemHost, KV: o.kv}
	if dmrHub != nil {
		modemAPI.Replica = dmrHub.InstanceID()
	}
	r.Use(middleware.Provider(v1ConfigControllers.ModemKey, modemAPI))
	r.Use(middleware.Provider("Ready", ready))
	r.Use(middleware.Provider("Hub", dmrHub))
	r.Use(middleware.Provider("NetScheduler", ns))
	r.Use(middleware.Provider("Version", version))
	r.Use(middleware.Provider("Commit", commit))
	if o.hibpClient != nil {
		r.Use(middleware.Provider(utils.HIBPClientKey, o.hibpClient))
	}
	if o.mailer != nil {
		r.Use(middleware.Provider(v1AuthControllers.MailerKey, o.mailer))
	} else {
		current := o.live.Current()
		r.Use(func(c *gin.Context) {
			c.Set(v1AuthControllers.MailerKey, smtp.NewMailer(current.Get()))
			c.Next()
		})
	}
	engine := o.pluginEngine
	if engine == nil {
		engine = wasm.NewEngine(wasm.DefaultLimits())
	}
	r.Use(middleware.Provider(v1PluginsControllers.EngineKey, engine))
	if o.kv != nil {
		r.Use(middleware.Provider(v1PluginsControllers.KVKey, o.kv))
	}
	// CORS
	var configuredCORS gin.HandlerFunc
	if config.HTTP.CORS.Enabled {
		corsConfig := cors.DefaultConfig()
		corsConfig.AllowCredentials = true
		corsConfig.AllowOrigins = config.HTTP.CORS.Hosts
		configuredCORS = cors.New(corsConfig)
	}
	r.Use(publicCORS(configuredCORS))

	// Sessions
	sessionStore := sessionstore.New(db, config.GetDerivedSecret())
	go sessionStore.PeriodicCleanup(ctx, time.Hour)
	r.Use(middleware.Provider("SessionStore", sessionStore))
	r.Use(sessions.Sessions(sessionstore.CookieName, sessionStore))
}

// currentConfig gives each request the config that DMRHub runs with when
// the request comes.
func currentConfig(current configPkg.Source) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("Config", current.Get())
		c.Next()
	}
}

func addSetupWizardMiddleware(config *configPkg.Config, state *setupwizard.State, r *gin.Engine, configCompleteChan chan any, version, commit string) {
	// Tracing
	if config.Metrics.OTLPEndpoint != "" {
		r.Use(otelgin.Middleware("setupwizard"))
		r.Use(middleware.TracingProvider(config))
	}

	r.Use(sameOrigin)
	r.Use(middleware.Provider(v1SetupWizardControllers.StateKey, state))
	r.Use(func(c *gin.Context) {
		if live := state.Live(); live != nil {
			c.Set("Config", live.Current().Get())
			c.Set(v1ConfigControllers.LiveKey, live)
		} else {
			c.Set("Config", config)
		}
		c.Next()
	})
	r.Use(middleware.Provider("SetupWizardConfigCompleteChan", configCompleteChan))

	// Versioning
	r.Use(middleware.Provider("Version", version))
	r.Use(middleware.Provider("Commit", commit))
}

// sameOrigin refuses a request that a page of another site sends. Anyone
// who opens the setup wizard may use it, but a page of another site must
// not use it through the browser of the visitor.
func sameOrigin(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		c.Next()
		return
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		c.Next()
		return
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host != c.Request.Host {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "The setup wizard takes requests from its own pages only"})
		return
	}
	c.Next()
}

// CreateSetupWizardRouter returns the router of the setup wizard, whose
// phase state holds.
func CreateSetupWizardRouter(config *configPkg.Config, state *setupwizard.State, configCompleteChan chan any, version, commit string) *gin.Engine {
	r := gin.New()
	// Logging middleware replaced or removed; consider using slog for access logs if needed
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	addSetupWizardMiddleware(config, state, r, configCompleteChan, version, commit)

	setupwizard.ApplyRoutes(config, r)

	addFrontendRoutes(r)

	return r
}

func CreateRouter(ctx context.Context, config *configPkg.Config, dmrHub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, ready *atomic.Bool, ns *netscheduler.NetScheduler, version, commit string, opts ...Option) *gin.Engine {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.live == nil {
		o.live = liveconfig.New(liveconfig.Options{Base: config, DB: db})
	}

	r := gin.New()
	// Logging middleware replaced or removed; consider using slog for access logs if needed
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	err := r.SetTrustedProxies(config.HTTP.TrustedProxies)
	if err != nil {
		slog.Error("Failed setting trusted proxies", "error", err)
	}

	addMiddleware(ctx, config, dmrHub, r, db, pubsub, ready, ns, version, commit, o)
	oauthServer := oauth.NewServer(db, config.GetDerivedSecret())
	go oauthServer.PeriodicPrune(ctx, time.Hour)
	r.Use(middleware.Provider(v1OAuthControllers.ServerKey, oauthServer))

	ratelimitStore := gormRateLimit.NewGORMStore(&gormRateLimit.GORMOptions{
		DB:    db,
		Rate:  rateLimitRate,
		Limit: rateLimitLimit,
	})
	ratelimitMW := ratelimit.RateLimiter(ratelimitStore, &ratelimit.Options{
		ErrorHandler: func(c *gin.Context, info ratelimit.Info) {
			c.String(http.StatusTooManyRequests, "Too many requests. Try again in "+time.Until(info.ResetTime).String())
		},
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
	})

	userLockoutMiddleware := middleware.SuspendedUserLockout()

	api.ApplyRoutes(o.live.Current(), r, dmrHub, db, pubsub, oauthServer, ratelimitMW, userLockoutMiddleware)

	addFrontendRoutes(r)

	return r
}
func (s *Server) Stop(ctx context.Context) {
	s.stopOnce.Do(func() {
		slog.Info("Stopping HTTP Server")
		const timeout = 5 * time.Second
		shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown HTTP server", "error", err)
		}
		<-s.shutdownChannel
	})
}

var ErrClosed = errors.New("server closed")
var ErrFailed = errors.New("failed to start server")

func (s *Server) Start() error {
	go func() {
		err := s.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Failed to start HTTP server", "error", err)
		}
		s.shutdownChannel <- true
	}()
	return nil
}

// isPublicPath reports whether any web page may read the API route path.
// Such a route answers any origin, without credentials, so that no browser
// sends a session cookie with a request from another site.
func isPublicPath(path string) bool {
	switch path {
	case "/api/v1/lastheard", "/api/v1/network/webtransport",
		"/.well-known/oauth-authorization-server", "/api/v1/oauth/token", "/api/v1/oauth/revoke":
		return true
	}
	return false
}

// isTokenRequest reports whether an app on another site calls an API route
// with a Bearer token, or asks in a preflight whether it may. The consent
// and the list of apps are only for the pages of DMRHub itself.
func isTokenRequest(c *gin.Context) bool {
	path := c.Request.URL.Path
	if !strings.HasPrefix(path, "/api/v1/") || strings.HasPrefix(path, "/api/v1/oauth/") {
		return false
	}
	if c.Request.Method == http.MethodOptions {
		for _, h := range strings.Split(c.GetHeader("Access-Control-Request-Headers"), ",") {
			if strings.EqualFold(strings.TrimSpace(h), "authorization") {
				return true
			}
		}
		return false
	}
	return c.GetHeader("Authorization") != ""
}

// publicCORS answers the CORS requests of the public routes and of apps
// with Bearer tokens for any origin, and hands every other request to
// configured, when it is set.
func publicCORS(configured gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Origin") == "" || (!isPublicPath(c.Request.URL.Path) && !isTokenRequest(c)) {
			if configured != nil {
				configured(c)
			}
			return
		}
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if c.Request.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "43200")
			c.AbortWithStatus(http.StatusNoContent)
		}
	}
}
