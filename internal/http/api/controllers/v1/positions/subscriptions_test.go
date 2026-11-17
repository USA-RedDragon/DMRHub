// SPDX-License-Identifier: AGPL-3.0-or-later

package positions_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocationSubscriptionOwnerAndQueue(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.track(owner)
	plugin, err := models.EnsureBuiltinPlugin(e.db, "locator", "Location server", "test", []models.PluginRange{{First: 5050, Last: 5059}}, time.Now())
	require.NoError(t, err)
	_, err = models.UpdatePlugin(e.db, plugin.ID, plugin.Name, true, plugin.Ranges)
	require.NoError(t, err)
	path := fmt.Sprintf("/api/v1/positions/%d/subscription", owner)
	otherJar := testutils.Login(t, e.router, "other")
	post := func(jar testutils.CookieJar, action string) int {
		w := testutils.Do(t, e.router, jar, http.MethodPost, path, map[string]string{"action": action})
		return w.Code
	}
	assert.Equal(t, http.StatusNotFound, e.get(otherJar, path, nil))
	assert.Equal(t, http.StatusNotFound, post(otherJar, "once"))
	assert.Equal(t, http.StatusBadRequest, post(e.jar, "invalid"))
	assert.Equal(t, http.StatusAccepted, post(e.jar, "subscribe"))
	assert.Equal(t, http.StatusTooManyRequests, post(e.jar, "once"))
	var out map[string]any
	assert.Equal(t, http.StatusOK, e.get(e.jar, path, &out))
	assert.Equal(t, "queued", out["state"])

	snapshot, err := json.Marshal(&pluginv1.LocationStatus{
		Radio: uint32(owner), RequestId: []byte{1, 2}, Service: 5050, State: "reporting", IntervalSeconds: 60,
		ReportedAt: time.Now().Add(-4 * time.Minute).UnixMilli(),
	})
	require.NoError(t, err)
	require.NoError(t, e.db.Create(&models.LocationSubscription{RadioID: owner, UserID: owner, Snapshot: snapshot}).Error)
	assert.Equal(t, http.StatusOK, e.get(e.jar, path, &out))
	assert.Equal(t, "stale", out["state"])
	assert.NotContains(t, out, "acknowledged_at_ms")
}
