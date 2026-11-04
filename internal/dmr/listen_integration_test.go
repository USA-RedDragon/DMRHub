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
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	listenws "github.com/USA-RedDragon/DMRHub/internal/http/api/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	listenTalker   = uint(1000001)
	listenRepeater = uint(100001)
	listenOther    = uint(100002)
)

// listenHub seeds two talkgroups and a talking repeater, starts the
// servers and an HTTP server with cfg changed by change, and returns the
// ws URL of the listen monitor and the connected repeater.
func listenHub(t *testing.T, stack *testutils.IntegrationStack, change func(*config.Config), seed ...func()) (string, *testutils.MMDVMClient) {
	t.Helper()
	stack.SeedUser(t, listenTalker, "N0CALL")
	stack.SeedTalkgroup(t, 1, "TG1")
	stack.SeedTalkgroup(t, 2, "TG2")
	stack.SeedMMDVMRepeater(t, listenRepeater, listenTalker, testPassword)
	for _, f := range seed {
		f()
	}
	stack.StartServers(t)
	c := testutils.NewMMDVMClient(uint32(listenRepeater), "", testPassword)
	require.NoError(t, c.Connect(stack.MMDVMAddr))
	t.Cleanup(c.Close)
	require.NoError(t, c.WaitReady(handshakeWait))

	cfg := *stack.Config
	if change != nil {
		change(&cfg)
	}
	ready := &atomic.Bool{}
	ready.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	router := internalhttp.CreateRouter(ctx, &cfg, stack.Hub, stack.DB, stack.PubSub, ready, nil, "test", "deadbeef", internalhttp.WithKV(stack.KV), testutils.Websockets(t))
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/listen", c
}

type listenConn struct {
	t    *testing.T
	conn *websocket.Conn
}

func dialListen(t *testing.T, url string) *listenConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deliverWait)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil) //nolint:bodyclose // coder/websocket closes the body of a switched connection
	require.NoError(t, err)
	conn.SetReadLimit(1 << 20)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return &listenConn{t: t, conn: conn}
}

type listenMsg struct {
	text   map[string]any
	packet models.Packet
	binary bool
}

// read returns the next message, or the error that ended the connection.
func (l *listenConn) read() (listenMsg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), deliverWait)
	defer cancel()
	typ, data, err := l.conn.Read(ctx)
	if err != nil {
		return listenMsg{}, fmt.Errorf("read: %w", err)
	}
	if typ == websocket.MessageBinary {
		p, ok := models.UnpackPacket(data)
		require.True(l.t, ok, "a binary message that is not a DMRD packet")
		return listenMsg{packet: p, binary: true}, nil
	}
	var m map[string]any
	require.NoError(l.t, json.Unmarshal(data, &m))
	return listenMsg{text: m}, nil
}

// until reads messages until done takes one, and returns the messages
// read.
func (l *listenConn) until(done func(listenMsg) bool) []listenMsg {
	l.t.Helper()
	var got []listenMsg
	for {
		m, err := l.read()
		require.NoError(l.t, err)
		got = append(got, m)
		if done(m) {
			return got
		}
	}
}

func (l *listenConn) hello() []uint {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deliverWait)
	defer cancel()
	_, data, err := l.conn.Read(ctx)
	require.NoError(l.t, err)
	var hello listenws.ListenHello
	require.NoError(l.t, json.Unmarshal(data, &hello))
	require.Equal(l.t, "hello", hello.Type)
	ids := make([]uint, 0, len(hello.Talkgroups))
	for _, tg := range hello.Talkgroups {
		ids = append(ids, tg.ID)
	}
	return ids
}

// tune tunes the session to tg and returns the answer of the hub.
func (l *listenConn) tune(tg uint) map[string]any {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deliverWait)
	defer cancel()
	require.NoError(l.t, l.conn.Write(ctx, websocket.MessageText, fmt.Appendf(nil, `{"tune":%d}`, tg)))
	got := l.until(func(m listenMsg) bool { return m.text["type"] == "tuned" })
	return got[len(got)-1].text
}

func (l *listenConn) refused() websocket.CloseError {
	l.t.Helper()
	for {
		_, err := l.read()
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		require.True(l.t, errors.As(err, &ce), "the session ended with %v, not a close frame", err)
		return ce
	}
}

func endOf(stream uint) func(listenMsg) bool {
	return func(m listenMsg) bool {
		return m.binary && m.packet.StreamID == stream && isTerminator(m.packet)
	}
}

func talk(t *testing.T, c *testutils.MMDVMClient, tg, stream uint) {
	t.Helper()
	require.NoError(t, c.SendDMRD(makeGroupVoicePacket(listenTalker, tg, stream, false)))
	require.NoError(t, c.SendDMRD(makeGroupVoiceTermPacket(listenTalker, tg, stream, false)))
}

// TestListenHearsTalkgroupCall tunes a listener to a talkgroup that no
// repeater links, and checks that it gets the bursts of a call to it as
// DMRD packets from no repeater, and the call with the callsign.
func TestListenHearsTalkgroupCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		url, c := listenHub(t, stack, nil)
		l := dialListen(t, url)
		assert.Equal(t, []uint{1, 2}, l.hello())
		assert.Equal(t, float64(1), l.tune(1)["talkgroup"])

		talk(t, c, 1, 701)
		var bursts []models.Packet
		var call map[string]any
		l.until(func(m listenMsg) bool {
			if m.binary {
				bursts = append(bursts, m.packet)
			} else if m.text["type"] == "call" && call == nil {
				call = m.text
			}
			return call != nil && len(bursts) > 0 && isTerminator(bursts[len(bursts)-1])
		})
		require.Len(t, bursts, 2)
		for _, p := range bursts {
			assert.Equal(t, uint(1), p.Dst)
			assert.Equal(t, listenTalker, p.Src)
			assert.True(t, p.GroupCall)
			assert.Zero(t, p.Repeater, "a burst names no repeater of the listener")
			assert.Equal(t, uint(701), p.StreamID)
		}
		assert.Equal(t, float64(1), call["talkgroup"])
		assert.Equal(t, float64(listenTalker), call["source"])
		assert.Equal(t, "N0CALL", call["callsign"])
	})
}

// TestListenSwitchesAndLeaves tunes a listener from one talkgroup to the
// other and to none. The calls of one source slot reach the monitor in the
// order they were routed, so a burst of a talkgroup it left would come
// before the call it waits for.
func TestListenSwitchesAndLeaves(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		url, c := listenHub(t, stack, func(cfg *config.Config) { cfg.Listen.PerAddress = 1 })
		l := dialListen(t, url)
		l.hello()
		l.tune(1)
		assert.Equal(t, float64(2), l.tune(2)["talkgroup"])
		talk(t, c, 1, 711)
		talk(t, c, 2, 712)
		for _, m := range l.until(endOf(712)) {
			if m.binary {
				assert.Equal(t, uint(2), m.packet.Dst, "a burst of the talkgroup it left")
			}
		}

		assert.Equal(t, float64(0), l.tune(0)["talkgroup"])
		talk(t, c, 2, 713)
		l.tune(1)
		talk(t, c, 1, 714)
		for _, m := range l.until(endOf(714)) {
			if m.binary {
				assert.Equal(t, uint(1), m.packet.Dst, "a burst while tuned to none")
			}
		}

		second := dialListen(t, url)
		ce := second.refused()
		assert.Equal(t, websocket.StatusPolicyViolation, ce.Code)
		assert.Equal(t, listenws.ListenRefusedAddress, ce.Reason)
		require.NoError(t, l.conn.Close(websocket.StatusNormalClosure, ""))
		var third *listenConn
		waitFor(t, "the hub to free the place of the listener that left", func() bool {
			third = dialListen(t, url)
			m, err := third.read()
			return err == nil && m.text["type"] == "hello"
		})
		third.tune(2)
		talk(t, c, 2, 715)
		third.until(endOf(715))
	})
}

// TestListenCannotTransmit sends a voice burst on a listen session. The
// hub closes it, and the burst reaches no repeater and no record: the
// session is not a radio or a repeater that the hub heard.
func TestListenCannotTransmit(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		url, c := listenHub(t, stack, nil, func() {
			stack.SeedMMDVMRepeater(t, listenOther, listenTalker, testPassword)
			stack.AssignTS1StaticTG(t, listenOther, 1)
		})
		other := testutils.NewMMDVMClient(uint32(listenOther), "", testPassword)
		require.NoError(t, other.Connect(stack.MMDVMAddr))
		defer other.Close()
		require.NoError(t, other.WaitReady(handshakeWait))
		waitSubscribed(t, stack.Hub, listenOther, 1, true)

		const intruder = uint(3001999)
		l := dialListen(t, url)
		l.hello()
		l.tune(1)
		burst := makeGroupVoicePacket(intruder, 1, 721, false)
		burst.Repeater = listenRepeater
		ctx, cancel := context.WithTimeout(context.Background(), deliverWait)
		defer cancel()
		require.NoError(t, l.conn.Write(ctx, websocket.MessageBinary, burst.Encode()))
		ce := l.refused()
		assert.Equal(t, websocket.StatusPolicyViolation, ce.Code)
		assert.Equal(t, listenws.ListenRefusedTransmit, ce.Reason)

		talk(t, c, 1, 722)
		for _, p := range receiveCall(other, 1) {
			assert.Equal(t, uint(722), p.StreamID, "the burst of the listen session reached a repeater")
		}
		stack.Hub.WaitForCalls()
		stack.CallTracker.Flush()
		var n int64
		require.NoError(t, stack.DB.Model(&models.Call{}).Where("stream_id = ? OR user_id = ?", 721, intruder).Count(&n).Error)
		assert.Zero(t, n, "the hub recorded a call of the listen session")
		_, err := stack.KV.Get(context.Background(), fmt.Sprintf("dmrhub:lastheard:%d", intruder))
		assert.Error(t, err, "the hub heard the listen session as a radio")
		kv := servers.MakeKVClient(stack.KV)
		for _, id := range []uint{0, intruder} {
			_, err := kv.GetRepeater(context.Background(), id)
			assert.Error(t, err, "the listen session has a session record as repeater %d", id)
		}
	})
}

// TestListenFollowsTalkgroupSetting takes a talkgroup off the monitor while
// a listener hears it, and checks the list, the tune and the listener.
func TestListenFollowsTalkgroupSetting(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		url, _ := listenHub(t, stack, nil)
		l := dialListen(t, url)
		l.hello()
		l.tune(2)
		require.NoError(t, stack.DB.Model(&models.Talkgroup{}).Where("id = ?", 2).Update("no_listen", true).Error)
		stack.Hub.TalkgroupChanged(2)
		got := l.until(func(m listenMsg) bool { return m.text["type"] == "tuned" })
		off := got[len(got)-1].text
		assert.Equal(t, float64(0), off["talkgroup"])
		assert.NotEmpty(t, off["error"])

		again := l.tune(2)
		assert.Equal(t, float64(0), again["talkgroup"])
		assert.NotEmpty(t, again["error"])
		assert.Equal(t, []uint{1}, dialListen(t, url).hello())
	})
}

// TestListenRefusedWhenOff checks that the monitor takes no listener while
// the listener limit is 0 or visitors see nothing.
func TestListenRefusedWhenOff(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*config.Config){
		"no listeners":       func(cfg *config.Config) { cfg.Listen.Listeners = 0 },
		"visibility nothing": func(cfg *config.Config) { cfg.Visibility = config.VisibilityLogin },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				url, _ := listenHub(t, stack, change)
				ce := dialListen(t, url).refused()
				assert.Equal(t, websocket.StatusPolicyViolation, ce.Code)
				assert.Equal(t, listenws.ListenRefusedOff, ce.Reason)
			})
		})
	}
}

// TestListenFollowsVisibility checks each visibility: a listener gets no
// more than a visitor on the last heard page. Everything shows the talker
// alias of a call, no names or positions drops it, and nothing refuses
// the session.
func TestListenFollowsVisibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		visibility config.Visibility
		alias      string
	}{
		{config.VisibilityPublic, "N0CALL Demo"},
		{config.VisibilityHideNames, ""},
		{config.VisibilityLogin, ""},
	} {
		t.Run(string(tc.visibility), func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				url, _ := listenHub(t, stack, func(cfg *config.Config) { cfg.Visibility = tc.visibility })
				l := dialListen(t, url)
				if tc.visibility == config.VisibilityLogin {
					ce := l.refused()
					assert.Equal(t, websocket.StatusPolicyViolation, ce.Code)
					assert.Equal(t, listenws.ListenRefusedOff, ce.Reason)
					return
				}
				l.hello()
				l.tune(1)
				lat := 32.7767
				call, err := json.Marshal(apimodels.WSCallResponse{
					ID: 1, Active: true, GroupCall: true, IsToTalkgroup: true, Kind: models.CallKindVoice,
					ToTalkgroup: apimodels.WSCallResponseTalkgroup{ID: 1}, SourceID: listenTalker,
					User:        apimodels.WSCallResponseUser{ID: listenTalker, Callsign: "N0CALL"},
					TalkerAlias: "N0CALL Demo", Latitude: &lat, Longitude: &lat,
				})
				require.NoError(t, err)
				require.NoError(t, stack.PubSub.Publish("calls:public", call))
				got := l.until(func(m listenMsg) bool { return m.text["type"] == "call" })
				m := got[len(got)-1].text
				assert.Equal(t, "N0CALL", m["callsign"])
				assert.Equal(t, tc.alias, m["alias"])
				assert.NotContains(t, m, "latitude")
			})
		})
	}
}
