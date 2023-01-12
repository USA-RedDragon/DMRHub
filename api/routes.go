package api

import (
	"fmt"

	v1Controllers "github.com/USA-RedDragon/dmrserver-in-a-box/api/controllers/v1"
	"github.com/gorilla/mux"
)

// ApplyRoutes to the HTTP Mux
func ApplyRoutes(router *mux.Router) {
	v1(router.PathPrefix(fmt.Sprintf("/api/v1/")).Subrouter())
}

func v1(router *mux.Router) {
	router.HandleFunc("/version", v1Controllers.GETVersion)
	router.HandleFunc("/ping", v1Controllers.GETPing)
}
