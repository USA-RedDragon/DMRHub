package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
)

func RedisProvider(redis *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("Redis", redis)
		c.Next()
	}
}
