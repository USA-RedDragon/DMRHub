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

package mmdvm

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func webTransportServer(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	return webTransportServerWith(t, func(*config.Config) {})
}

func webTransportServerWith(t *testing.T, set func(*config.Config)) (*Server, *config.Config) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = ""
	cfg.Database.ExtraParameters = []string{}
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	cfg.DMR.MMDVM.WebTransport = config.MMDVMWebTransport{Enabled: true, Port: 0}
	set(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })
	for _, id := range []uint{311860, 311861} {
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: id},
			Type:                  models.RepeaterTypeMMDVM,
			Password:              "s3cret",
		}).Error)
	}
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return &s, &cfg
}

// dialByHash opens a WebTransport session to the server and accepts its
// certificate only by one of the hashes that it publishes, as a browser
// with serverCertificateHashes does.
func dialByHash(t *testing.T, s *Server, cfg *config.Config) *webtransport.Session {
	t.Helper()
	status, sess := dialFrom(t, s, cfg, "")
	require.Equal(t, 200, status)
	return sess
}

// dialFrom opens a WebTransport session as a page of origin does, and
// returns the status of the answer and the session when it opened. An
// empty origin sends no Origin header.
func dialFrom(t *testing.T, s *Server, cfg *config.Config, origin string) (int, *webtransport.Session) {
	t.Helper()
	certs, err := wtcert.New(cfg.GetDerivedSecret(), "", "")
	require.NoError(t, err)
	hashes, err := certs.Hashes()
	require.NoError(t, err)
	tlsConf := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec
		VerifyConnection: func(state tls.ConnectionState) error {
			got := sha256.Sum256(state.PeerCertificates[0].Raw)
			for _, h := range hashes {
				if got == h {
					return nil
				}
			}
			return errors.New("the certificate has no published hash")
		},
	}
	d := &webtransport.Transport{TLSClientConfig: tlsConf, QUICConfig: &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true}}
	t.Cleanup(func() { _ = d.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	rsp, sess, err := d.Dial(ctx, fmt.Sprintf("https://%s%s", s.WebTransportAddr().String(), wtcert.Path), header)
	if err != nil {
		require.NotNil(t, rsp, "no answer: %v", err)
		return rsp.StatusCode, nil
	}
	t.Cleanup(func() { _ = sess.CloseWithError(0, "") })
	return rsp.StatusCode, sess
}

func TestWebTransportTakesEveryOriginByDefault(t *testing.T) {
	t.Parallel()
	s, cfg := webTransportServer(t)
	for _, origin := range []string{"", "https://radio.example", "https://elsewhere.example"} {
		status, _ := dialFrom(t, s, cfg, origin)
		assert.Equal(t, http.StatusOK, status, "origin %q", origin)
	}
}

func TestWebTransportTakesOnlyTheAllowedOrigins(t *testing.T) {
	t.Parallel()
	s, cfg := webTransportServerWith(t, func(c *config.Config) {
		c.DMR.MMDVM.WebTransport.AllowedOrigins = []string{"HTTPS://Radio.Example:443/", "http://localhost:8080"}
	})
	for origin, want := range map[string]int{
		"https://radio.example":     http.StatusOK,
		"https://RADIO.example:443": http.StatusOK,
		"http://localhost:8080":     http.StatusOK,
		"http://localhost":          http.StatusForbidden,
		"http://radio.example":      http.StatusForbidden,
		"https://elsewhere.example": http.StatusForbidden,
		"null":                      http.StatusForbidden,
		"":                          http.StatusForbidden,
	} {
		status, _ := dialFrom(t, s, cfg, origin)
		assert.Equal(t, want, status, "origin %q", origin)
	}
}

func rptlAck(t *testing.T, sess *webtransport.Session, id uint32) []byte {
	t.Helper()
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	require.NoError(t, sess.SendDatagram(p))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := sess.ReceiveDatagram(ctx)
	require.NoError(t, err)
	return b
}

func TestWebTransportCarriesTheMMDVMLogin(t *testing.T) {
	t.Parallel()
	s, cfg := webTransportServer(t)
	one := dialByHash(t, s, cfg)
	two := dialByHash(t, s, cfg)

	ack := rptlAck(t, one, 311860)
	assert.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(ack, dmrd.DialectMMDVM), "an RPTL in a datagram gets its RPTACK in a datagram")
	ack2 := rptlAck(t, two, 311861)
	assert.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(ack2, dmrd.DialectMMDVM), "each session is its own peer")

	s.mu.Lock()
	n := 0
	for key, l := range s.links {
		if l.ep.transport == webTransportName {
			n++
			assert.Contains(t, key, webTransportName+"/", "a WebTransport session is keyed apart from UDP addresses")
		}
	}
	s.mu.Unlock()
	assert.Equal(t, 2, n)
}

func TestWebTransportIsOffByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	assert.False(t, cfg.DMR.MMDVM.WebTransport.Enabled)
}
