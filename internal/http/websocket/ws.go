package websocket

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/config"
	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/http/api/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type WSHandler struct {
	wsUpgrader websocket.Upgrader
	redis      *redis.Client
}

func CreateHandler(db *gorm.DB, redis *redis.Client) *WSHandler {
	return &WSHandler{
		redis: redis,
		wsUpgrader: websocket.Upgrader{
			HandshakeTimeout: 0,
			ReadBufferSize:   1024,
			WriteBufferSize:  1024,
			WriteBufferPool:  nil,
			Subprotocols:     []string{},
			Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
			},
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return false
				}
				for _, host := range config.GetConfig().CORSHosts {
					if strings.HasSuffix(host, ":443") && strings.HasPrefix(origin, "https://") {
						host = strings.TrimSuffix(host, ":443")
					}
					if strings.HasSuffix(host, ":80") && strings.HasPrefix(origin, "http://") {
						host = strings.TrimSuffix(host, ":80")
					}
					if strings.Contains(origin, host) {
						return true
					}
				}
				return false
			},
			EnableCompression: true,
		},
	}
}

func (h *WSHandler) pingHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer conn.Close()

	for {
		t, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if string(msg) == "PING" {
			msg = []byte("PONG")
		}
		conn.WriteMessage(t, msg)
	}
}

func (h *WSHandler) repeaterHandler(ctx context.Context, db *gorm.DB, session sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer conn.Close()

	for {
		t, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		conn.WriteMessage(t, msg)
	}
}

func (h *WSHandler) callHandler(ctx context.Context, db *gorm.DB, session sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer conn.Close()

	userIDIface := session.Get("user_id")
	var pubsub *redis.PubSub
	if userIDIface == nil {
		// User ID not found, subscribe to TG calls
		pubsub = h.redis.Subscribe(ctx, "calls")
		defer pubsub.Unsubscribe(ctx, "calls")
	} else {
		userID := userIDIface.(uint)
		pubsub = h.redis.Subscribe(ctx, fmt.Sprintf("calls:%d", userID))
		defer pubsub.Unsubscribe(ctx, fmt.Sprintf("calls:%d", userID))
	}
	defer pubsub.Close()

	readFailed := false
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				readFailed = true
				break
			}
		}
	}()

	go func() {
		for msg := range pubsub.Channel() {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				klog.Errorf("Failed to write message to websocket: %v", err)
				return
			}
		}
	}()

	for {
		if readFailed {
			pubsub.Close()
			break
		}
	}
}

func (h *WSHandler) ApplyRoutes(r *gin.Engine) {
	r.GET("/ws/repeaters", middleware.RequireLogin(), func(c *gin.Context) {
		db := c.MustGet("DB").(*gorm.DB)
		session := sessions.Default(c)
		h.repeaterHandler(c.Request.Context(), db, session, c.Writer, c.Request)
	})

	r.GET("/ws/health", func(c *gin.Context) {
		h.pingHandler(c.Writer, c.Request)
	})

	r.GET("/ws/calls", func(c *gin.Context) {
		db := c.MustGet("DB").(*gorm.DB)
		session := sessions.Default(c)
		h.callHandler(c.Request.Context(), db, session, c.Writer, c.Request)
	})
}
