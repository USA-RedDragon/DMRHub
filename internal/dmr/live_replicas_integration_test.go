// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package dmr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveBackends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()}
}

// putSetting saves value at the dotted path in the config that the config
// API of r serves, as the Setup page does.
func putSetting(t *testing.T, r *testutils.LiveReplica, values map[string]any) (int, apimodels.ConfigSaveResponse, map[string]any) {
	t.Helper()
	cfg := r.GetConfig(t)
	for path, v := range values {
		parts := strings.Split(path, ".")
		m := cfg
		for _, p := range parts[:len(parts)-1] {
			next, ok := m[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				m[p] = next
			}
			m = next
		}
		m[parts[len(parts)-1]] = v
	}
	w := testutils.Do(t, r.Router, r.Admin, http.MethodPut, "/api/v1/config", cfg)
	var res apimodels.ConfigSaveResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res), w.Body.String())
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	return w.Code, res, raw
}

func statusOf(t *testing.T, r *testutils.LiveReplica) apimodels.ConfigResponse {
	t.Helper()
	w := testutils.Do(t, r.Router, r.Admin, http.MethodGet, "/api/v1/config", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res apimodels.ConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	return res
}

func replicaOf(res apimodels.ConfigSaveResponse, id string) (apimodels.ConfigSaveReplica, bool) {
	for _, r := range res.Replicas {
		if r.ID == id {
			return r, true
		}
	}
	return apimodels.ConfigSaveReplica{}, false
}

func idOf(t *testing.T, r *testutils.LiveReplica) string {
	t.Helper()
	st := statusOf(t, r)
	for _, rep := range st.Replicas {
		if rep.Self {
			return rep.ID
		}
	}
	t.Fatal("the config API must name its own replica")
	return ""
}

// TestLiveConfigReachesEveryReplica saves settings on one replica. Every
// replica puts them into effect, the save waits for and returns the report
// of each, and the other replica serves them as saved in Setup.
func TestLiveConfigReachesEveryReplica(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range liveBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{})
			ipscBefore := b.Manager.Server(manager.IPSC)
			bID := idOf(t, b)

			code, res, raw := putSetting(t, l.First(), map[string]any{
				"dmr.ipsc.network-id": 888888, "dmr.retention.messages": 30, "network-name": "Shared",
			})
			require.Equal(t, http.StatusOK, code, raw)
			require.Len(t, res.Replicas, 2)
			assert.True(t, res.Replicas[0].Self)
			other, ok := replicaOf(res, bID)
			require.True(t, ok, "the save must report the other replica")
			assert.True(t, other.Answered)
			assert.Equal(t, res.Revision, other.Revision)
			assert.Empty(t, other.ApplyErrors)
			assert.Empty(t, res.ApplyErrors)

			got := b.Live.Current().Get()
			assert.Equal(t, uint32(888888), got.DMR.IPSC.NetworkID)
			assert.Equal(t, 30, got.DMR.Retention.Messages)
			assert.Equal(t, "Shared", got.NetworkName)
			assert.NotSame(t, ipscBefore, b.Manager.Server(manager.IPSC), "the IPSC server of the other replica must restart with the new network ID")

			st := statusOf(t, b)
			assert.Equal(t, uint32(888888), st.DMR.IPSC.NetworkID)
			assert.Equal(t, liveconfig.SourceSetup, st.Settings["dmr.ipsc.network-id"].Source)
			assert.True(t, st.Settings["dmr.ipsc.network-id"].Editable)
			assert.False(t, st.Settings["dmr.ipsc.network-id"].Disagree)
			for _, rep := range st.Replicas {
				assert.True(t, rep.Current, "replica %s must run the latest save", rep.ID)
			}
		})
	}
}

// TestLiveConfigLockedReplicaKeepsItsValue runs a replica whose
// environment sets the IPSC network ID. A save on the other replica leaves
// it its own value, the config API shows that the replicas disagree, and
// Setup on the locked replica refuses a change of the setting.
func TestLiveConfigLockedReplicaKeepsItsValue(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range liveBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			a := l.First()
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{
				Change: func(c *config.Config) { c.DMR.IPSC.NetworkID = 777777 },
				Locks: liveconfig.Locks{
					"dmr.ipsc.network-id": {Source: liveconfig.SourceEnv},
					"dmr.mmdvm.port":      {Source: liveconfig.SourceEnv},
					"dmr.ipsc.port":       {Source: liveconfig.SourceEnv},
					"dmr.openbridge.port": {Source: liveconfig.SourceEnv},
				},
			})
			bID := idOf(t, b)

			code, res, raw := putSetting(t, a, map[string]any{"dmr.ipsc.network-id": 888888})
			require.Equal(t, http.StatusOK, code, raw)
			other, ok := replicaOf(res, bID)
			require.True(t, ok)
			assert.True(t, other.Answered)
			assert.Empty(t, other.ApplyErrors)
			assert.Equal(t, uint32(888888), a.Live.Current().Get().DMR.IPSC.NetworkID)
			assert.Equal(t, uint32(777777), b.Live.Current().Get().DMR.IPSC.NetworkID)

			st := statusOf(t, a)
			assert.True(t, st.Disagree)
			id := st.Settings["dmr.ipsc.network-id"]
			assert.True(t, id.Disagree)
			require.Len(t, id.Replicas, 2)
			values := map[string]string{}
			sources := map[string]liveconfig.Source{}
			for _, v := range id.Replicas {
				values[v.Replica] = string(v.Value)
				sources[v.Replica] = v.Source
			}
			assert.Equal(t, "777777", values[bID])
			assert.Equal(t, liveconfig.SourceEnv, sources[bID])
			assert.False(t, st.Settings["network-name"].Disagree)

			bst := statusOf(t, b)
			bid := bst.Settings["dmr.ipsc.network-id"]
			assert.True(t, bid.Locked)
			assert.False(t, bid.Editable)
			assert.Equal(t, liveconfig.SourceEnv, bid.Source)
			assert.JSONEq(t, "888888", string(bid.Saved))
			assert.False(t, bid.NotInEffect)

			code, _, raw = putSetting(t, b, map[string]any{"dmr.ipsc.network-id": 999999})
			require.Equal(t, http.StatusBadRequest, code, raw)
			assert.Contains(t, raw["errors"], "dmr")
			assert.Contains(t, toJSON(t, raw["errors"]), "set by the deployment")

			code, _, raw = putSetting(t, b, map[string]any{"network-name": "From B"})
			require.Equal(t, http.StatusOK, code, raw)
			assert.Equal(t, "From B", a.Live.Current().Get().NetworkName)
			assert.Equal(t, uint32(888888), a.Live.Current().Get().DMR.IPSC.NetworkID, "a save on the locked replica keeps the saved value of its locked setting")
		})
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// TestLiveConfigReportsAReplicaThatFails saves an MMDVM port that the
// first replica binds, so that the other replica on the same host cannot.
// The save answers 409 with the report of the replica that failed, which
// keeps its old port, and the config API of that replica shows the
// setting not in effect.
func TestLiveConfigReportsAReplicaThatFails(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range liveBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			a := l.First()
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{Locks: liveconfig.Locks{
				"dmr.ipsc.port":       {Source: liveconfig.SourceEnv},
				"dmr.openbridge.port": {Source: liveconfig.SourceEnv},
			}})
			bID := idOf(t, b)
			bAddr := b.Addr(t, manager.MMDVM)

			port := testutils.FreeUDPPort(t)
			code, res, raw := putSetting(t, a, map[string]any{"dmr.mmdvm.port": port})
			require.Equal(t, http.StatusConflict, code, raw)
			assert.Equal(t, loopback(port), a.Addr(t, manager.MMDVM))
			assert.Equal(t, bAddr, b.Addr(t, manager.MMDVM), "the replica that failed keeps its old port")
			other, ok := replicaOf(res, bID)
			require.True(t, ok)
			assert.True(t, other.Answered)
			require.Len(t, other.ApplyErrors, 1)
			assert.Equal(t, "mmdvm", other.ApplyErrors[0].Server)
			assert.True(t, other.ApplyErrors[0].Running)
			require.Len(t, res.ApplyErrors, 1)
			assert.Equal(t, bID, res.ApplyErrors[0].Replica)
			assert.Contains(t, res.ApplyErrors[0].Error, "address already in use")

			st := statusOf(t, b)
			assert.True(t, st.Settings["dmr.mmdvm.port"].NotInEffect)
			assert.JSONEq(t, toJSON(t, port), string(st.Settings["dmr.mmdvm.port"].Saved))
			for _, rep := range st.Replicas {
				if rep.ID == bID {
					assert.Len(t, rep.ApplyErrors, 1)
				}
			}
			assert.True(t, statusOf(t, a).Settings["dmr.mmdvm.port"].Disagree)
		})
	}
}

// TestLiveConfigReportsAReplicaThatCannotRunTheSave saves IPSC off with no
// network ID. The other replica, whose environment turns IPSC on, cannot
// run without one: it keeps its settings and reports why.
func TestLiveConfigReportsAReplicaThatCannotRunTheSave(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	l := testutils.SetupLiveStack(t)
	a := l.First()
	b := l.SpawnLiveReplica(t, testutils.LiveOptions{Locks: liveconfig.Locks{
		"dmr.ipsc.enabled":    {Source: liveconfig.SourceEnv},
		"dmr.mmdvm.port":      {Source: liveconfig.SourceEnv},
		"dmr.ipsc.port":       {Source: liveconfig.SourceEnv},
		"dmr.openbridge.port": {Source: liveconfig.SourceEnv},
	}})
	bID := idOf(t, b)

	code, res, raw := putSetting(t, a, map[string]any{"dmr.ipsc.enabled": false, "dmr.ipsc.network-id": 0, "network-name": "Changed"})
	require.Equal(t, http.StatusConflict, code, raw)
	other, ok := replicaOf(res, bID)
	require.True(t, ok)
	require.Len(t, other.Invalid, 1)
	assert.Equal(t, "dmr.ipsc.network-id", other.Invalid[0].Field)
	require.Len(t, res.ApplyErrors, 1)
	assert.Equal(t, "dmr.ipsc.network-id", res.ApplyErrors[0].Field)
	assert.Nil(t, a.Manager.Server(manager.IPSC))
	assert.NotNil(t, b.Manager.Server(manager.IPSC))
	assert.Equal(t, uint32(999999), b.Live.Current().Get().DMR.IPSC.NetworkID)
	assert.Equal(t, "DMRHub", b.Live.Current().Get().NetworkName, "a replica that cannot run the save keeps all of its settings")
}

// TestLiveConfigReplicaTakesASaveItMissed saves with a replica that
// announces nothing, as when the pubsub drops the message. The other
// replica finds the save at its next check.
func TestLiveConfigReplicaTakesASaveItMissed(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range liveBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{Every: 100 * time.Millisecond})

			quiet := liveconfig.New(liveconfig.Options{Base: l.Config, DB: l.DB})
			require.NoError(t, quiet.Boot(context.Background()))
			next := *quiet.Current().Get()
			next.DMR.Retention.Bursts = 7
			_, errs, err := quiet.Save(context.Background(), &next)
			require.NoError(t, err)
			require.Empty(t, errs)

			assert.Eventually(t, func() bool { return b.Live.Current().Get().DMR.Retention.Bursts == 7 }, 5*time.Second, 20*time.Millisecond)
			assert.Eventually(t, func() bool { return l.Live.Current().Get().DMR.Retention.Bursts == 7 }, 5*time.Second, 20*time.Millisecond)
		})
	}
}

// TestLiveConfigSavesNoStartOnlySetting refuses a change of the HTTP port
// and of a secret in Setup, and keeps the secrets out of the database and
// of the records of the replicas.
func TestLiveConfigSavesNoStartOnlySetting(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	l := testutils.SetupLiveStack(t)
	a := l.First()
	code, _, raw := putSetting(t, a, map[string]any{"http.port": 8080})
	require.Equal(t, http.StatusBadRequest, code, raw)
	assert.Contains(t, toJSON(t, raw["errors"]), "start of DMRHub only")
	code, _, raw = putSetting(t, a, map[string]any{"secret": "another-secret-that-is-long-enough"})
	require.Equal(t, http.StatusBadRequest, code, raw)

	code, _, raw = putSetting(t, a, map[string]any{"network-name": "Net"})
	require.Equal(t, http.StatusOK, code, raw)
	saved, err := liveconfig.LoadSaved(context.Background(), l.DB)
	require.NoError(t, err)
	for path := range saved.Values {
		assert.NotContains(t, []string{"secret", "password-salt", "http.port"}, path)
	}
	st := statusOf(t, a)
	assert.True(t, st.Settings["secret"].StartOnly)
	assert.NotContains(t, toJSON(t, st), l.Config.Secret, "the config API must not show a secret")
}

// TestLiveConfigNamesAReplicaThatDoesNotAnswer saves while the record of
// a replica that stopped answering is still in the KV store. The save
// waits for it no longer than its wait and names it as not answered.
func TestLiveConfigNamesAReplicaThatDoesNotAnswer(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	l := testutils.SetupLiveStackWith(t, testutils.LiveOptions{Wait: 300 * time.Millisecond})
	ghost, err := json.Marshal(liveconfig.Report{Replica: "ghost", Revision: 0})
	require.NoError(t, err)
	require.NoError(t, l.KV.Set(context.Background(), "dmrhub:liveconfig:replica:ghost", ghost))

	start := time.Now()
	code, res, raw := putSetting(t, l.First(), map[string]any{"network-name": "Net"})
	require.Equal(t, http.StatusOK, code, raw)
	assert.Less(t, time.Since(start), 3*time.Second)
	g, ok := replicaOf(res, "ghost")
	require.True(t, ok)
	assert.False(t, g.Answered)
	assert.Equal(t, "Net", l.Live.Current().Get().NetworkName)
}
