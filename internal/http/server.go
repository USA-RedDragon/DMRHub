// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	redisSessions "github.com/USA-RedDragon/DMRHub/internal/http/sessions"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	ratelimit "github.com/USA-RedDragon/gin-rate-limit-v9"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

var (
	ErrReadDir = errors.New("error reading directory")
)

type Server struct {
	*http.Server
	shutdownChannel chan bool
}

const defTimeout = 10 * time.Second
const debugWriteTimeout = 60 * time.Second
const rateLimitRate = time.Second
const rateLimitLimit = 10

func MakeServer(db *gorm.DB, redisClient *redis.Client) Server {
	if config.GetConfig().Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := CreateRouter(db, redisClient)

	writeTimeout := defTimeout
	if config.GetConfig().Debug {
		writeTimeout = debugWriteTimeout
	}

	logging.Errorf("HTTP Server listening at %s on port %d\n", config.GetConfig().ListenAddr, config.GetConfig().HTTPPort)
	s := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.GetConfig().ListenAddr, config.GetConfig().HTTPPort),
		Handler:      r,
		ReadTimeout:  defTimeout,
		WriteTimeout: writeTimeout,
	}
	s.SetKeepAlivesEnabled(false)

	return Server{
		s,
		make(chan bool),
	}
}

// FS is the embedded frontend files
//
//go:embed frontend/dist/*
var FS embed.FS

func addMiddleware(r *gin.Engine, db *gorm.DB, redisClient *redis.Client) {
	// Debug
	if config.GetConfig().Debug {
		pprof.Register(r)
	}

	// Tracing
	if config.GetConfig().OTLPEndpoint != "" {
		r.Use(otelgin.Middleware("api"))
		r.Use(middleware.TracingProvider())
	}

	// DBs
	r.Use(middleware.DatabaseProvider(db))
	r.Use(middleware.PaginatedDatabaseProvider(db, middleware.PaginationConfig{}))
	r.Use(middleware.RedisProvider(redisClient))

	// CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowOrigins = config.GetConfig().CORSHosts
	r.Use(cors.New(corsConfig))

	// Sessions
	sessionStore, _ := redisSessions.NewStore(redisClient, []byte(""), config.GetConfig().Secret)
	r.Use(sessions.Sessions("sessions", sessionStore))
}

func CreateRouter(db *gorm.DB, redisClient *redis.Client) *gin.Engine {
	if config.GetConfig().Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.LoggerWithWriter(logging.GetLogger(logging.AccessType).Writer))
	r.Use(gin.Recovery())

	err := r.SetTrustedProxies(config.GetConfig().TrustedProxies)
	if err != nil {
		logging.Errorf("Failed setting trusted proxies: %v", err)
	}

	addMiddleware(r, db, redisClient)

	ratelimitStore := ratelimit.RedisStore(&ratelimit.RedisOptions{
		RedisClient: redisClient,
		Rate:        rateLimitRate,
		Limit:       rateLimitLimit,
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

	api.ApplyRoutes(r, db, redisClient, ratelimitMW, userLockoutMiddleware)

	addFrontendRoutes(r)

	return r
}

func addFrontendWildcards(staticGroup *gin.RouterGroup, depth int) {
	staticGroup.GET("/", func(c *gin.Context) {
		file, err := FS.Open("frontend/dist/index.html")
		if err != nil {
			logging.Errorf("Failed to open file: %s", err)
			return
		}
		defer func() {
			err := file.Close()
			if err != nil {
				logging.Errorf("Failed to close file: %s", err)
			}
		}()
		fileContent, getErr := io.ReadAll(file)
		if getErr != nil {
			logging.Errorf("Failed to read file: %s", getErr)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	wildcard := "/:wild"
	for i := 0; i < depth; i++ {
		// We need to make a string that contains /:wild for each depth
		// After the first depth, we need to add a number to the end of the wild
		// Example for depth 3: /:wild/:wild2/:wild3
		if i > 0 {
			wildcard += fmt.Sprintf("/:wild%d", i)
		}
		thisDepth := i
		staticGroup.GET(wildcard, func(c *gin.Context) {
			wildPath := "frontend/dist"
			// We need to get the wildcards and add them to the path
			// Example for depth 3: /:wild/:wild2/:wild3

			// Get the first wildcard
			wild, have := c.Params.Get("wild")
			if !have {
				logging.Errorf("Failed to get wildcard")
				return
			}
			// Add the first wildcard to the path
			wildPath = path.Join(wildPath, wild)

			if thisDepth > 0 {
				// Get the rest of the wildcards
				for j := 1; j <= thisDepth; j++ {
					wild, have := c.Params.Get(fmt.Sprintf("wild%d", j))
					if !have {
						logging.Errorf("Failed to get wildcard")
						return
					}
					wildPath = path.Join(wildPath, wild)
				}
			}
			file, fileErr := FS.Open(wildPath)
			if fileErr != nil {
				file, fileErr = FS.Open("frontend/dist/index.html")
				if fileErr != nil {
					logging.Errorf("Failed to open file: %s", fileErr)
					return
				}
			}
			defer func() {
				err := file.Close()
				if err != nil {
					logging.Errorf("Failed to close file: %s", err)
				}
			}()
			fileContent, readErr := io.ReadAll(file)
			if readErr != nil {
				logging.Errorf("Failed to read file: %s", readErr)
				return
			}
			c.Data(http.StatusOK, "text/html", fileContent)
		})
	}
}

func addFrontendRoutes(r *gin.Engine) {
	staticGroup := r.Group("/")

	files, err := getAllFilenames(&FS, "frontend/dist")
	if err != nil {
		logging.Errorf("Failed to read directory: %s", err)
	}
	const wildcardDepth = 4
	addFrontendWildcards(staticGroup, wildcardDepth)
	for _, entry := range files {
		staticName := strings.Replace(entry, "frontend/dist", "", 1)
		if staticName == "" {
			continue
		}
		staticGroup.GET(staticName, func(c *gin.Context) {
			file, fileErr := FS.Open(fmt.Sprintf("frontend/dist%s", c.Request.URL.Path))
			if fileErr != nil {
				logging.Errorf("Failed to open file: %s", fileErr)
				return
			}
			defer func() {
				err = file.Close()
				if err != nil {
					logging.Errorf("Failed to close file: %s", err)
				}
			}()
			fileContent, fileErr := io.ReadAll(file)
			if fileErr != nil {
				logging.Errorf("Failed to read file: %s", fileErr)
				return
			}
			handleMime(c, fileContent, entry)
		})
	}
}

func handleMime(c *gin.Context, fileContent []byte, entry string) {
	switch {
	case strings.HasSuffix(c.Request.URL.Path, ".js"):
		c.Data(http.StatusOK, "text/javascript", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".css"):
		c.Data(http.StatusOK, "text/css", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".html") || strings.HasSuffix(entry, ".htm"):
		c.Data(http.StatusOK, "text/html", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".ico"):
		c.Data(http.StatusOK, "image/x-icon", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".png"):
		c.Data(http.StatusOK, "image/png", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".jpg") || strings.HasSuffix(entry, ".jpeg"):
		c.Data(http.StatusOK, "image/jpeg", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".webp"):
		c.Data(http.StatusOK, "image/webp", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".svg"):
		c.Data(http.StatusOK, "image/svg+xml", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".gif"):
		c.Data(http.StatusOK, "image/gif", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".json"):
		c.Data(http.StatusOK, "application/json", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".xml"):
		c.Data(http.StatusOK, "text/xml", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".txt"):
		c.Data(http.StatusOK, "text/plain", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".webmanifest"):
		c.Data(http.StatusOK, "application/manifest+json", fileContent)
		return
	default:
		c.Data(http.StatusOK, "text/plain", fileContent)
		return
	}
}

func (s *Server) Stop() {
	logging.Logf("Stopping HTTP Server")
	const timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		logging.Errorf("Failed to shutdown HTTP server: %s", err)
	}
	<-s.shutdownChannel
}

var ErrClosed = errors.New("Server closed")
var ErrFailed = errors.New("Failed to start server")

func (s *Server) Start() error {
	g := new(errgroup.Group)
	g.Go(func() error {
		err := s.ListenAndServe()
		if err != nil {
			switch {
			case errors.Is(err, http.ErrServerClosed):
				s.shutdownChannel <- true
				return ErrClosed
			default:
				logging.Errorf("Failed to start HTTP server: %s", err)
				return ErrFailed
			}
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return err //nolint:golint,wrapcheck
	}
	return nil
}

func getAllFilenames(fs *embed.FS, dir string) ([]string, error) {
	if len(dir) == 0 {
		dir = "."
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, ErrReadDir
	}

	out := make([]string, len(entries))

	for _, entry := range entries {
		fp := path.Join(dir, entry.Name())
		if entry.IsDir() {
			res, err := getAllFilenames(fs, fp)
			if err != nil {
				return nil, err
			}
			out = append(out, res...)
			continue
		}
		out = append(out, fp)
	}

	return out, nil
}
