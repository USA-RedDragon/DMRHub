package websocket

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/USA-RedDragon/dmrserver-in-a-box/config"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http/api/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type WSHandler struct {
	wsUpgrader websocket.Upgrader
	redis      *redis.Client
}

func CreateHandler(db *gorm.DB, allowedOrigins []string) *WSHandler {
	return &WSHandler{
		redis: redis.NewClient(&redis.Options{
			Addr: config.GetConfig().RedisHost,
		}),
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
				for _, host := range allowedOrigins {
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

func (h *WSHandler) repeaterHandler(db *gorm.DB, session sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}

	for {
		t, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		conn.WriteMessage(t, msg)
	}
}

func (h *WSHandler) callHandler(db *gorm.DB, session sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}

	userIDIface := session.Get("user_id")
	if userIDIface == nil {
		// User ID not found, subscribe to TG calls
		pubsub := h.redis.Subscribe("calls")
		defer pubsub.Close()
		for msg := range pubsub.Channel() {
			conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
		}
	} else {
		userID := userIDIface.(uint)

		pubsub := h.redis.Subscribe(fmt.Sprintf("calls:%d", userID))
		defer pubsub.Close()
		for msg := range pubsub.Channel() {
			conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
		}
	}

}

func (h *WSHandler) ApplyRoutes(r *gin.Engine) {
	r.GET("/ws/repeaters", middleware.RequireLogin(), func(c *gin.Context) {
		db := c.MustGet("DB").(*gorm.DB)
		session := sessions.Default(c)
		h.repeaterHandler(db, session, c.Writer, c.Request)
	})

	r.GET("/ws/health", func(c *gin.Context) {
		h.pingHandler(c.Writer, c.Request)
	})

	r.GET("/ws/calls", func(c *gin.Context) {
		db := c.MustGet("DB").(*gorm.DB)
		session := sessions.Default(c)
		h.callHandler(db, session, c.Writer, c.Request)
	})
}
