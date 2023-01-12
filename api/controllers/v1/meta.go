package v1

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
)

func GETVersion(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintf("%s-%s", sdk.Version, sdk.GitCommit))
}

func GETPing(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintf("%d", time.Now().Unix()))
}
