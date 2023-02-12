package websocket

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
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

func (h *WSHandler) pingHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			klog.Errorf("Failed to close websocket: %v", err)
		}
	}()

	readFailed := make(chan string)
	go func() {
		for {
			t, msg, err := conn.ReadMessage()
			if err != nil {
				readFailed <- "read failed"
				break
			}
			if string(msg) == "PING" {
				msg = []byte("PONG")
			}
			err = conn.WriteMessage(t, msg)
			if err != nil {
				readFailed <- "write failed"
				break
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readFailed:
	}
}

func (h *WSHandler) repeaterHandler(ctx context.Context, _ sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			klog.Errorf("Failed to close websocket: %v", err)
		}
	}()

	readFailed := make(chan string)
	go func() {
		for {
			t, msg, err := conn.ReadMessage()
			if err != nil {
				readFailed <- "read failed"
				break
			}
			err = conn.WriteMessage(t, msg)
			if err != nil {
				readFailed <- "write failed"
				break
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readFailed:
	}
}

func (h *WSHandler) callHandler(ctx context.Context, session sessions.Session, w http.ResponseWriter, r *http.Request) {
	conn, err := h.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		klog.Errorf("Failed to set websocket upgrade: %v", err)
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			klog.Errorf("Failed to close websocket: %v", err)
		}
	}()

	userIDIface := session.Get("user_id")
	var pubsub *redis.PubSub
	if userIDIface == nil {
		// User ID not found, subscribe to TG calls
		pubsub = h.redis.Subscribe(ctx, "calls")
		defer func() {
			err := pubsub.Unsubscribe(ctx, "calls")
			if err != nil {
				klog.Errorf("Failed to unsubscribe from calls: %v", err)
			}
		}()
	} else {
		userID, ok := userIDIface.(uint)
		if !ok {
			klog.Errorf("Failed to convert user ID to uint")
			return
		}
		pubsub = h.redis.Subscribe(ctx, fmt.Sprintf("calls:%d", userID))
		defer func() {
			err := pubsub.Unsubscribe(ctx, fmt.Sprintf("calls:%d", userID))
			if err != nil {
				klog.Errorf("Failed to unsubscribe from calls: %v", err)
			}
		}()
	}
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Failed to close pubsub: %v", err)
		}
	}()

	readFailed := make(chan string)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				readFailed <- "read failed"
				break
			}
		}
	}()

	go func() {
		for msg := range pubsub.Channel() {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				klog.Errorf("Failed to write message to websocket: %v", err)
				readFailed <- "write failed"
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-readFailed:
	}
}

func (h *WSHandler) ApplyRoutes(r *gin.Engine, ratelimit gin.HandlerFunc) {
	r.GET("/ws/repeaters", middleware.RequireLogin(), ratelimit, func(c *gin.Context) {
		session := sessions.Default(c)
		h.repeaterHandler(c.Request.Context(), session, c.Writer, c.Request)
	})

	r.GET("/ws/health", ratelimit, func(c *gin.Context) {
		h.pingHandler(c.Request.Context(), c.Writer, c.Request)
	})

	r.GET("/ws/calls", ratelimit, func(c *gin.Context) {
		session := sessions.Default(c)
		h.callHandler(c.Request.Context(), session, c.Writer, c.Request)
	})
}
