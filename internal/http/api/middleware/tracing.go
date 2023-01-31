package middleware

import (
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TracingProvider() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.GetConfig().OTLPEndpoint != "" {
			ctx := c.Request.Context()
			span := trace.SpanFromContext(ctx)
			if span.IsRecording() {
				span.SetAttributes(
					attribute.String("http.method", c.Request.Method),
					attribute.String("http.path", c.Request.URL.Path),
				)
			}
		}
		c.Next()
	}
}
