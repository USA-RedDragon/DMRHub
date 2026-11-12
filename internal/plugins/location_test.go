// SPDX-License-Identifier: AGPL-3.0-or-later

package plugins_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltInLocatorClaimsCommandsAndRestoresPeriodicEvidence(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		fake := clock.NewFake(time.Now())
		r := newRig(t, plugins.WithClock(fake))
		p, err := models.EnsureBuiltinPlugin(r.db, "locator", "Location server", "test", []models.PluginRange{rng(5050, 5059)}, fake.Now())
		require.NoError(t, err)
		require.NoError(t, r.registry.Reload())
		require.NoError(t, r.db.Create(&models.User{ID: trackedRadio, Callsign: "N6CALL", Username: "tracked", Approved: true}).Error)
		require.NoError(t, models.SetPositionTracking(r.db, trackedRadio, trackedRadio, true, fake.Now()))
		cmd, queued, err := models.QueueLocationCommand(r.db, trackedRadio, trackedRadio, "subscribe", fake.Now())
		require.NoError(t, err)
		require.True(t, queued)
		connect := func() (*innerStream, func()) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			s := newInnerStream(plugins.InProcess(ctx, p.ID))
			done := make(chan error, 1)
			go func() { done <- r.svc.Connect(s) }()
			welcome := bubble.Receive(t, s.out, time.Second, "Locator welcome").GetWelcome()
			require.NotNil(t, welcome)
			assert.Contains(t, welcome.GetFeatures(), pluginv1.Feature_FEATURE_LOCATION_SUBSCRIPTIONS)
			s.in <- &pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: &pluginv1.Subscribe{Tracking: true}}}
			return s, func() {
				cancel()
				require.NoError(t, bubble.Receive(t, done, time.Second, "Locator disconnect"))
			}
		}
		s, disconnect := connect()
		require.NotNil(t, bubble.Receive(t, s.out, time.Second, "tracked radios").GetTracked())
		fake.Advance(5 * time.Second)
		action := bubble.Receive(t, s.out, time.Second, "location command").GetLocationCommand()
		require.NotNil(t, action)
		assert.Equal(t, cmd.ID, action.GetId())

		s.in <- &pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_LocationStatus{LocationStatus: &pluginv1.LocationStatus{
			Radio: uint32(trackedRadio), State: "pending", Service: locatorID, CommandId: cmd.ID, SentAt: fake.Now().UnixMilli(),
		}}}
		s.in <- &pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_LocationStatus{LocationStatus: &pluginv1.LocationStatus{
			Radio: uint32(trackedRadio), State: "accepted", Service: locatorID, RequestId: []byte{1, 2, 3, 4},
			AcknowledgedAt: fake.Now().UnixMilli(), IntervalSeconds: 600, Slot: 2,
		}}}
		synctest.Wait()
		var saved models.LocationCommand
		require.NoError(t, r.db.First(&saved, trackedRadio).Error)
		assert.Equal(t, "pending", saved.State)
		var evidence models.LocationSubscription
		require.NoError(t, r.db.First(&evidence, trackedRadio).Error)
		var status pluginv1.LocationStatus
		require.NoError(t, json.Unmarshal(evidence.Snapshot, &status))
		assert.Equal(t, "accepted", status.GetState())

		disconnect()
		s, disconnect = connect()
		defer disconnect()
		restored := bubble.Receive(t, s.out, time.Second, "restored location evidence").GetLocationStatus()
		require.NotNil(t, restored)
		assert.Equal(t, []byte{1, 2, 3, 4}, restored.GetRequestId())
		require.NotNil(t, bubble.Receive(t, s.out, time.Second, "tracked radios").GetTracked())
		require.NoError(t, r.db.First(&saved, trackedRadio).Error)
		assert.Equal(t, "failed", saved.State)
	})
}
