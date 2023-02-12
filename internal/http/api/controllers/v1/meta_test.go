package v1_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/sdk"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestPingRoute(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	// Convert ts (time.Now().Unix()) to int64
	var tsInt int64
	fmt.Sscanf(w.Body.String(), "%d", &tsInt)

	w = httptest.NewRecorder()

	time.Sleep(1 * time.Second)

	req, _ = http.NewRequest("GET", "/api/v1/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var tsInt2 int64
	fmt.Sscanf(w.Body.String(), "%d", &tsInt2)

	assert.Greater(t, tsInt2, tsInt)
}

func TestVersionRoute(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/version", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	assert.Equal(t, fmt.Sprintf("%s-%s", sdk.Version, sdk.GitCommit), w.Body.String())
}
