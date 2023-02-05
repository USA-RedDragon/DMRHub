package http

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-contrib/pprof"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	redis "github.com/USA-RedDragon/DMRHub/internal/http/sessions"
	websocketHandler "github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	ratelimit "github.com/USA-RedDragon/gin-rate-limit-v9"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	realredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

//go:embed frontend/dist/*
var FS embed.FS

var ws *websocketHandler.WSHandler

// Start the HTTP server
func Start(db *gorm.DB, redisClient *realredis.Client) {
	if config.GetConfig().Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	ws = websocketHandler.CreateHandler(db, redisClient)

	// Setup API
	r := gin.Default()
	r.SetTrustedProxies(config.GetConfig().TrustedProxies)

	if config.GetConfig().Debug {
		pprof.Register(r)
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r.Use(middleware.DatabaseProvider(db))
	r.Use(middleware.PaginatedDatabaseProvider(db, middleware.PaginationConfig{}))
	r.Use(middleware.RedisProvider(redisClient))

	ratelimitStore := ratelimit.RedisStore(&ratelimit.RedisOptions{
		RedisClient: redisClient,
		Rate:        time.Second,
		Limit:       10,
	})
	ratelimitMW := ratelimit.RateLimiter(ratelimitStore, &ratelimit.Options{
		ErrorHandler: func(c *gin.Context, info ratelimit.Info) {
			c.String(429, "Too many requests. Try again in "+time.Until(info.ResetTime).String())
		},
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
	})

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowOrigins = config.GetConfig().CORSHosts
	r.Use(cors.New(corsConfig))

	sessionStore, _ := redis.NewStore(redisClient, []byte(""), config.GetConfig().Secret)
	r.Use(sessions.Sessions("sessions", sessionStore))

	ws.ApplyRoutes(r, ratelimitMW)

	r.Use(otelgin.Middleware("api"))
	r.Use(middleware.TracingProvider())

	api.ApplyRoutes(r, ratelimitMW)

	staticGroup := r.Group("/")

	files, err := getAllFilenames(&FS, "frontend/dist")
	if err != nil {
		klog.Errorf("Failed to read directory: %s", err)
	}
	staticGroup.GET("/", func(c *gin.Context) {
		file, getErr := FS.Open("frontend/dist/index.html")
		if getErr != nil {
			klog.Errorf("Failed to open file: %s", getErr)
		}
		defer file.Close()
		fileContent, getErr := io.ReadAll(file)
		if getErr != nil {
			klog.Errorf("Failed to read file: %s", getErr)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	staticGroup.GET("/:wild", func(c *gin.Context) {
		wild, have := c.Params.Get("wild")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		file, fileErr := FS.Open(path.Join("frontend/dist", wild))
		if fileErr != nil {
			file, fileErr = FS.Open("frontend/dist/index.html")
			if fileErr != nil {
				klog.Errorf("Failed to open file: %s", fileErr)
				return
			}
		}
		defer file.Close()
		fileContent, readErr := io.ReadAll(file)
		if readErr != nil {
			klog.Errorf("Failed to read file: %s", readErr)
			return
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	staticGroup.GET("/:wild/:wild2", func(c *gin.Context) {
		wild, have := c.Params.Get("wild")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		wild2, have := c.Params.Get("wild2")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		file, insideErr := FS.Open(path.Join("frontend/dist", wild, wild2))
		if insideErr != nil {
			file, insideErr = FS.Open("frontend/dist/index.html")
			if insideErr != nil {
				klog.Errorf("Failed to open file: %s", err)
				return
			}
		}
		defer file.Close()
		fileContent, nextErr := io.ReadAll(file)
		if nextErr != nil {
			klog.Errorf("Failed to read file: %s", nextErr)
			return
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	staticGroup.GET("/:wild/:wild2/:wild3", func(c *gin.Context) {
		wild, have := c.Params.Get("wild")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		wild2, have := c.Params.Get("wild2")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		wild3, have := c.Params.Get("wild3")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		file, err := FS.Open(path.Join("frontend/dist", wild, wild2, wild3))
		if err != nil {
			file, err = FS.Open("frontend/dist/index.html")
			if err != nil {
				klog.Errorf("Failed to open file: %s", err)
				return
			}
		}
		defer file.Close()
		fileContent, err := io.ReadAll(file)
		if err != nil {
			klog.Errorf("Failed to read file: %s", err)
			return
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	for _, entry := range files {
		staticName := strings.Replace(entry, "frontend/dist", "", 1)
		staticGroup.GET(staticName, func(c *gin.Context) {
			file, err := FS.Open(fmt.Sprintf("frontend/dist%s", c.Request.URL.Path))
			if err != nil {
				klog.Errorf("Failed to open file: %s", err)
				return
			}
			defer file.Close()
			fileContent, err := io.ReadAll(file)
			if err != nil {
				klog.Errorf("Failed to read file: %s", err)
				return
			}
			if strings.HasSuffix(c.Request.URL.Path, ".js") {
				c.Data(http.StatusOK, "text/javascript", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".css") {
				c.Data(http.StatusOK, "text/css", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".html") || strings.HasSuffix(entry, ".htm") {
				c.Data(http.StatusOK, "text/html", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".ico") {
				c.Data(http.StatusOK, "image/x-icon", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".png") {
				c.Data(http.StatusOK, "image/png", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".jpg") || strings.HasSuffix(entry, ".jpeg") {
				c.Data(http.StatusOK, "image/jpeg", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".webp") {
				c.Data(http.StatusOK, "image/webp", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".svg") {
				c.Data(http.StatusOK, "image/svg+xml", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".gif") {
				c.Data(http.StatusOK, "image/gif", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".json") {
				c.Data(http.StatusOK, "application/json", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".xml") {
				c.Data(http.StatusOK, "text/xml", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".txt") {
				c.Data(http.StatusOK, "text/plain", fileContent)
				return
			} else if strings.HasSuffix(c.Request.URL.Path, ".webmanifest") {
				c.Data(http.StatusOK, "application/manifest+json", fileContent)
				return
			} else {
				c.Data(http.StatusOK, "text/plain", fileContent)
				return
			}
		})
	}

	writeTimeout := 10 * time.Second
	if config.GetConfig().Debug {
		writeTimeout = 60 * time.Second
	}

	klog.Infof("HTTP Server listening at %s on port %d\n", config.GetConfig().ListenAddr, config.GetConfig().HTTPPort)
	s := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.GetConfig().ListenAddr, config.GetConfig().HTTPPort),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: writeTimeout,
	}
	s.SetKeepAlivesEnabled(false)

	s.ListenAndServe()
}

func getAllFilenames(fs *embed.FS, dir string) (out []string, err error) {
	if len(dir) == 0 {
		dir = "."
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, err
	}

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

	return
}
