package http

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/gin-contrib/pprof"

	"github.com/USA-RedDragon/dmrserver-in-a-box/config"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/middleware"
	websocketHandler "github.com/USA-RedDragon/dmrserver-in-a-box/http/websocket"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

//go:embed frontend/dist/*
var FS embed.FS

var ws *websocketHandler.WSHandler

// Start the HTTP server
func Start(host string, port int, verbose bool, db *gorm.DB, corsHosts []string) {
	ws = websocketHandler.CreateHandler(db, corsHosts)

	// Setup API
	r := gin.Default()
	pprof.Register(r)
	r.Use(middleware.DatabaseProvider(db))

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowOrigins = corsHosts
	r.Use(cors.New(corsConfig))

	store, _ := redis.NewStore(10, "tcp", config.GetConfig().RedisHost, "", []byte(config.GetConfig().Secret))
	r.Use(sessions.Sessions("sessions", store))

	ws.ApplyRoutes(r)
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
		}
		file, err := FS.Open(path.Join("frontend/dist", wild))
		if err != nil {
			file, err = FS.Open("frontend/dist/index.html")
			if err != nil {
				klog.Errorf("Failed to open file: %s", err)
			}
		}
		fileContent, err := io.ReadAll(file)
		if err != nil {
			klog.Errorf("Failed to read file: %s", err)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	staticGroup.GET("/:wild/:wild2", func(c *gin.Context) {
		wild, have := c.Params.Get("wild")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
		}
		wild2, have := c.Params.Get("wild2")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
		}
		file, err := FS.Open(path.Join("frontend/dist", wild, wild2))
		if err != nil {
			file, err = FS.Open("frontend/dist/index.html")
			if err != nil {
				klog.Errorf("Failed to open file: %s", err)
			}
		}
		fileContent, err := io.ReadAll(file)
		if err != nil {
			klog.Errorf("Failed to read file: %s", err)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	staticGroup.GET("/:wild/:wild2/:wild3", func(c *gin.Context) {
		wild, have := c.Params.Get("wild")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
		}
		wild2, have := c.Params.Get("wild2")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
		}
		wild3, have := c.Params.Get("wild3")
		if !have {
			klog.Errorf("Failed to get wildcard: %s", err)
		}
		file, err := FS.Open(path.Join("frontend/dist", wild, wild2, wild3))
		if err != nil {
			file, err = FS.Open("frontend/dist/index.html")
			if err != nil {
				klog.Errorf("Failed to open file: %s", err)
			}
		}
		fileContent, err := io.ReadAll(file)
		if err != nil {
			klog.Errorf("Failed to read file: %s", err)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	for _, entry := range files {
		staticName := strings.Replace(entry, "frontend/dist", "", 1)
		if verbose {
			klog.Infof("Entry: %s\n", staticName)
		}
		staticGroup.GET(staticName, func(c *gin.Context) {
			file, err := FS.Open(fmt.Sprintf("frontend/dist%s", c.Request.URL.Path))
			if err != nil {
				klog.Errorf("Failed to open file: %s", err)
			}
			fileContent, err := io.ReadAll(file)
			if err != nil {
				klog.Errorf("Failed to read file: %s", err)
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

	klog.Infof("HTTP Server listening at %s on port %d\n", host, port)
	http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), r)
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
