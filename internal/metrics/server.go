package metrics

import (
        "fmt"
        "net/http"
	"github.com/USA-RedDragon/DMRHub/internal/config"

        "github.com/prometheus/client_golang/prometheus/promhttp"
)

func CreateMetricsServer() {
	port := config.GetConfig().MetricsPort
	if port != 0 {
		http.Handle("/metrics", promhttp.Handler())
        	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	}
}

