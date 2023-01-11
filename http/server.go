package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// Start the HTTP server
func Start(host string, port int) {
	router := mux.NewRouter().StrictSlash(true)

	// Setup frontend as a fallback
	router.PathPrefix("/").Handler(
		handleUI(
			http.FileServer(
				&UIAssetWrapper{FileSystem: assetFS()})))

	http.Handle("/", router)

	fmt.Printf("Server listening at %s on port %d\n", host, port)
	http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), router)
}

func handleUI(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path = strings.TrimSuffix(req.URL.Path, "/")
		h.ServeHTTP(w, req)
		return
	})
}
