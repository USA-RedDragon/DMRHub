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

//go:build linux

package config_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	configcontroller "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type modemHost struct{ st *modem.Status }

func (h modemHost) ModemStatus() (modem.Status, bool) {
	if h.st == nil {
		return modem.Status{}, false
	}
	return *h.st, true
}

func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = unix.Close(fd)
		t.Skipf("cannot unlock the pseudo terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(fd)
		t.Skipf("no pseudo terminal number: %v", err)
	}
	slave := "/dev/pts/" + strconv.Itoa(n)
	hold, err := unix.Open(slave, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		_ = unix.Close(fd)
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(hold) })
	return os.NewFile(uintptr(fd), "/dev/ptmx"), slave
}

type modemAPI struct {
	router *gin.Engine
	opens  *atomic.Int32
}

func newModemAPI(t *testing.T, m *modem.Replicas) modemAPI {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	opens := &atomic.Int32{}
	m.Probe = modem.Options{Reply: 200 * time.Millisecond, Open: func(path string, baud int) (modem.Port, error) {
		opens.Add(1)
		return modem.OpenSerial(path, baud)
	}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("Config", &cfg)
		c.Set(configcontroller.ModemKey, m)
	})
	r.GET("/modem", configcontroller.GETModem)
	r.GET("/modem/ports", configcontroller.GETModemPorts)
	r.POST("/modem/test", configcontroller.POSTModemTest)
	return modemAPI{router: r, opens: opens}
}

func modemBody(t *testing.T, port string, baud int) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"port": port, "baud": baud, "rx-frequency": 438800000, "tx-frequency": 438800000})
	require.NoError(t, err)
	return b
}

func (a modemAPI) test(t *testing.T, body []byte) (int, modem.TestAnswer) {
	t.Helper()
	w := serve(t, a.router, http.MethodPost, "/modem/test", body, nil)
	var out modem.TestAnswer
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return w.Code, out
}

func TestModemTestReadsTheBoard(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	fake := modemtest.New(master, 1)
	t.Cleanup(fake.Close)
	api := newModemAPI(t, &modem.Replicas{})
	code, out := api.test(t, modemBody(t, slave, 115200))
	require.Equal(t, http.StatusOK, code)
	require.False(t, out.Live)
	require.NotNil(t, out.Result)
	r := out.Result
	assert.True(t, r.Answered)
	assert.Equal(t, "MMDVM fake 20261026", r.Firmware)
	assert.Equal(t, uint8(1), r.Protocol)
	assert.Equal(t, []string{"DMR", "D-Star", "YSF", "P25", "NXDN", "POCSAG"}, r.Modes)
	assert.True(t, r.Frequencies.Taken)
	assert.True(t, r.Settings.Taken)
	assert.Empty(t, r.Error)
}

func TestModemTestOfABoardThatDoesNotAnswer(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	t.Cleanup(func() { _ = master.Close() })
	api := newModemAPI(t, &modem.Replicas{})
	code, out := api.test(t, modemBody(t, slave, 57600))
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, out.Result)
	assert.True(t, out.Result.Opened)
	assert.False(t, out.Result.Answered)
	assert.Equal(t, 6, out.Result.Tries)
	assert.Equal(t, slave, out.Result.Port)
	assert.Equal(t, 57600, out.Result.Baud)
	assert.Nil(t, out.Result.Settings)
}

func TestModemTestRefusesSettingsThatDoNotWork(t *testing.T) {
	t.Parallel()
	api := newModemAPI(t, &modem.Replicas{})
	w := serve(t, api.router, http.MethodPost, "/modem/test", modemBody(t, "", 1200), nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"port"`)
	assert.Contains(t, w.Body.String(), `"baud"`)
	assert.NotContains(t, w.Body.String(), "repeater-id", "a test needs no repeater ID")
	assert.Zero(t, api.opens.Load())
}

// TestModemTestOfThePortOfTheRunningModem shows the live state of the
// modem that holds the port, on this replica or on another, and opens
// nothing.
func TestModemTestOfThePortOfTheRunningModem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local := &modem.Status{Replica: "r1", Port: "/dev/ttyAMA0", Baud: 115200, Error: "no version reply from the modem"}
	api := newModemAPI(t, &modem.Replicas{Host: modemHost{st: local}, ID: "r1"})
	code, out := api.test(t, modemBody(t, "/dev/ttyAMA0", 115200))
	require.Equal(t, http.StatusOK, code)
	require.True(t, out.Live)
	require.NotNil(t, out.State)
	assert.True(t, out.State.Self)
	assert.Equal(t, *local, *out.State.Status)
	assert.Zero(t, api.opens.Load())

	store, err := kv.MakeKV(ctx, &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.Modem.Port, cfg.DMR.Modem.RepeaterID = "/dev/ttyACM0", 311860
	cfg.DMR.Modem.RXFrequency, cfg.DMR.Modem.TXFrequency = 438800000, 438800000
	srv := modem.NewServerWithOptions(&cfg, nil, nil, "test", modem.Options{KV: store, Replica: "r2", Reply: 200 * time.Millisecond,
		Reopen: time.Hour, Retry: time.Hour, Open: func(string, int) (modem.Port, error) {
			host, dev := net.Pipe()
			f := modemtest.New(dev, 2)
			t.Cleanup(f.Close)
			return host, nil
		}})
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { _ = srv.Stop(ctx) })
	remote := newModemAPI(t, &modem.Replicas{KV: store, ID: "r1"})
	require.Eventually(t, func() bool {
		w := serve(t, remote.router, http.MethodGet, "/modem", nil, nil)
		var st modem.State
		return w.Code == http.StatusOK && json.Unmarshal(w.Body.Bytes(), &st) == nil && st.Status != nil && st.Status.Linked
	}, 5*time.Second, 10*time.Millisecond)
	code, out = remote.test(t, modemBody(t, "/dev/ttyACM0", 115200))
	require.Equal(t, http.StatusOK, code)
	require.True(t, out.Live)
	assert.False(t, out.State.Self)
	assert.Equal(t, "r2", out.State.Status.Replica)
	assert.Equal(t, "MMDVM fake 20261026", out.State.Status.Firmware)
	assert.Zero(t, remote.opens.Load())

	master, slave := openPTY(t)
	fake := modemtest.New(master, 2)
	t.Cleanup(fake.Close)
	code, out = remote.test(t, modemBody(t, slave, 115200))
	require.Equal(t, http.StatusOK, code)
	require.False(t, out.Live, "a modem on another port does not hold this one")
	assert.True(t, out.Result.Answered)
}

func TestGETModemWithNoModem(t *testing.T) {
	t.Parallel()
	api := newModemAPI(t, &modem.Replicas{})
	w := serve(t, api.router, http.MethodGet, "/modem", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":null,"self":false}`, w.Body.String())
}

func TestGETModemPorts(t *testing.T) {
	t.Parallel()
	api := newModemAPI(t, &modem.Replicas{ID: "r1", Ports: func() []modem.SerialPort {
		return []modem.SerialPort{{Path: "/dev/serial/by-id/usb-MMDVM-if00", Device: "/dev/ttyACM0"}, {Path: "/dev/ttyAMA0"}}
	}})
	w := serve(t, api.router, http.MethodGet, "/modem/ports", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"replicas":[{"replica":"r1","self":true,"ports":[{"path":"/dev/serial/by-id/usb-MMDVM-if00","device":"/dev/ttyACM0"},{"path":"/dev/ttyAMA0"}]}]}`, w.Body.String())
}

// TestModemTestOnAnotherReplica runs the test on the replica of the query,
// which shares the KV store and the pubsub with this one, lists the ports
// of both, and answers that a replica that is gone did not answer.
func TestModemTestOnAnotherReplica(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := kv.MakeKV(ctx, &config.Config{})
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(ctx, &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = store.Close()
	})
	regs := []*servers.InstanceRegistry{servers.NewInstanceRegistry(ctx, store, "r1"), servers.NewInstanceRegistry(ctx, store, "r2")}
	t.Cleanup(func() {
		for _, r := range regs {
			r.Deregister(ctx)
		}
	})
	master, slave := openPTY(t)
	fake := modemtest.New(master, 2)
	t.Cleanup(fake.Close)
	other := &modem.Replicas{ID: "r2", PubSub: ps, KV: store, Probe: modem.Options{Open: modem.OpenSerial, Reply: 200 * time.Millisecond},
		Ports: func() []modem.SerialPort { return []modem.SerialPort{{Path: slave}} }}
	other.Start(ctx)
	t.Cleanup(other.Stop)
	api := newModemAPI(t, &modem.Replicas{ID: "r1", PubSub: ps, KV: store, PortsWait: time.Second, TestWait: 2 * time.Second,
		Ports: func() []modem.SerialPort { return []modem.SerialPort{{Path: "/dev/ttyAMA0"}} }})

	w := serve(t, api.router, http.MethodGet, "/modem/ports", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"replicas":[{"replica":"r1","self":true,"ports":[{"path":"/dev/ttyAMA0"}]},{"replica":"r2","self":false,"ports":[{"path":"`+slave+`"}]}]}`, w.Body.String())

	w = serve(t, api.router, http.MethodPost, "/modem/test?replica=r2", modemBody(t, slave, 115200), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var out modem.TestAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "r2", out.Replica)
	assert.False(t, out.Self)
	require.NotNil(t, out.Result)
	assert.True(t, out.Result.Answered)
	assert.Zero(t, api.opens.Load(), "this replica opened no port")

	other.Stop()
	w = serve(t, api.router, http.MethodPost, "/modem/test?replica=r2", modemBody(t, slave, 115200), nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"replica":"r2","self":false,"unanswered":true,"live":false}`, w.Body.String())
}
