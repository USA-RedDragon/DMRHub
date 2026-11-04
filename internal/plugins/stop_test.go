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

package plugins_test

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/gorm"
)

// slowTexts is a Texts whose SendText waits for its context to end, and
// then for release, as a send that is still in the database would.
type slowTexts struct {
	entered, stopping, release chan struct{}
}

func (s *slowTexts) SendText(ctx context.Context, _, _ uint, _ string) (string, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.stopping)
	<-s.release
	return "", context.Cause(ctx) //nolint:wrapcheck // the error of the request's context
}

func (s *slowTexts) ReplyText(ctx context.Context, source uint, _ uint64, radio uint, body string) (string, error) {
	return s.SendText(ctx, source, radio, body)
}

// TestTheStreamOfAPluginEndsAfterItsRequests ends the stream of a plugin
// while the hub sends a text for it. The stream must end only after the
// send, so that nothing of the stream uses the database once it ended.
func TestTheStreamOfAPluginEndsAfterItsRequests(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		slow := &slowTexts{entered: make(chan struct{}), stopping: make(chan struct{}), release: make(chan struct{})}
		_, _, s, _ := sdkRig(t, plugins.WithTexts(slow))
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: &pluginv1.Request{
			Id: 99, Op: &pluginv1.Request_SendText{SendText: &pluginv1.SendText{Source: 4700, Radio: 1000001, Body: "hi"}},
		}}}))
		<-slow.entered
		ended := make(chan struct{})
		go func() {
			defer close(ended)
			for {
				if _, err := s.Recv(); err != nil {
					return
				}
			}
		}()
		require.NoError(t, s.CloseSend())
		<-slow.stopping
		synctest.Wait()
		select {
		case <-ended:
			assert.Fail(t, "the stream ended while the hub still sent a text for the plugin")
		default:
		}
		close(slow.release)
		<-ended
	})
}

// TestAStopThatCutsAStreamWaitsForItsRequests stops a listener whose
// plugin keeps its stream past the graceful wait, while the hub sends a
// text for it. Stop cuts the stream, and must return only after the send
// ended, so that nothing of the stream uses the database after Stop.
func TestAStopThatCutsAStreamWaitsForItsRequests(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		slow := &slowTexts{entered: make(chan struct{}), stopping: make(chan struct{}), release: make(chan struct{})}
		lis := bufconn.Listen(1 << 20)
		srv, err := plugins.ListenOn(lis, []byte("a secret of the config, long enough"), r.db, r.ps, r.router, plugins.WithTexts(slow))
		require.NoError(t, err)
		_, token := r.add("APRS", rng(4700, 4709))
		conn, err := grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})), //nolint:gosec // the test's own certificate
		)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		s, err := pluginv1.NewPluginServiceClient(conn).Connect(metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+token))
		require.NoError(t, err)
		m, err := s.Recv()
		require.NoError(t, err)
		require.NotNil(t, m.GetWelcome())
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: &pluginv1.Request{
			Id: 1, Op: &pluginv1.Request_SendText{SendText: &pluginv1.SendText{Source: 4700, Radio: 1000001, Body: "hi"}},
		}}}))
		<-slow.entered
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			srv.Stop()
		}()
		<-slow.stopping
		synctest.Wait()
		select {
		case <-stopped:
			assert.Fail(t, "Stop returned while a request of the stream it cut still ran")
		default:
		}
		close(slow.release)
		<-stopped
	})
}

// TestAStreamThatEndsCutsTheQueriesOfItsRequests ends the stream of a
// plugin while the database holds a query of its store. The query must end
// with the stream, so that a stuck query does not keep the stream, and a
// stop of the hub, waiting.
func TestAStreamThatEndsCutsTheQueriesOfItsRequests(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, _, s, _ := sdkRig(t)
		entered, release := make(chan struct{}), make(chan struct{})
		require.NoError(t, r.db.Callback().Query().Before("gorm:query").Register("test:hold", func(tx *gorm.DB) {
			if tx.Statement.Table != "plugin_store_entries" {
				return
			}
			close(entered)
			select {
			case <-tx.Statement.Context.Done():
				_ = tx.AddError(tx.Statement.Context.Err())
			case <-release:
			}
		}))
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: &pluginv1.Request{
			Id: 99, Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{Key: "held"}},
		}}}))
		<-entered
		ended := make(chan struct{})
		go func() {
			defer close(ended)
			for {
				if _, err := s.Recv(); err != nil {
					return
				}
			}
		}()
		require.NoError(t, s.CloseSend())
		synctest.Wait()
		select {
		case <-ended:
		default:
			assert.Fail(t, "the stream waited for a query that its end did not cut")
		}
		close(release)
		<-ended
	})
}
