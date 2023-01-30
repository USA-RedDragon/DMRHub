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

	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/config"
	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/http/api"
	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/http/api/middleware"
	redis "github.com/USA-RedDragon/dmrserver-in-a-box/internal/http/sessions"
	websocketHandler "github.com/USA-RedDragon/dmrserver-in-a-box/internal/http/websocket"
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
	ws = websocketHandler.CreateHandler(db, redisClient)

	// Setup API
	r := gin.Default()
	pprof.Register(r)
	r.Use(middleware.DatabaseProvider(db))
	r.Use(middleware.RedisProvider(redisClient))

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowOrigins = config.GetConfig().CORSHosts
	r.Use(cors.New(corsConfig))

	store, _ := redis.NewStore(redisClient, []byte(""), []byte(config.GetConfig().Secret))
	r.Use(sessions.Sessions("sessions", store))

	ws.ApplyRoutes(r)

	r.Use(otelgin.Middleware("api"))
	r.Use(middleware.TracingProvider())

	api.ApplyRoutes(r)

	staticGroup := r.Group("/")

	files, err := getAllFilenames(&FS, "frontend/dist")
	if err != nil {
		klog.Errorf("Failed to read directory: %s", err)
	}
	staticGroup.GET("/", func(c *gin.Context) {
		file, err := FS.Open("frontend/dist/index.html")
		if err != nil {
			klog.Errorf("Failed to open file: %s", err)
		}
		defer file.Close()
		fileContent, err := io.ReadAll(file)
		if err != nil {
			klog.Errorf("Failed to read file: %s", err)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	staticGroup.GET("/:wild", func(c *gin.Context) {
		wild, have := c.Params.Get("wild")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
			return
		}
		file, err := FS.Open(path.Join("frontend/dist", wild))
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
		file, err := FS.Open(path.Join("frontend/dist", wild, wild2))
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
		if config.GetConfig().Verbose {
			klog.Infof("Entry: %s\n", staticName)
		}
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

	klog.Infof("HTTP Server listening at %s on port %d\n", config.GetConfig().ListenAddr, config.GetConfig().HTTPPort)
	s := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.GetConfig().ListenAddr, config.GetConfig().HTTPPort),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
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
