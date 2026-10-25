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

package wasm

import (
	"context"
	"errors"
	"fmt"
	"io"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/grpc/metadata"
)

// errNotAMessage is returned by SendMsg and RecvMsg for a value that is not
// a message of the plugin protocol.
var errNotAMessage = errors.New("not a message of the plugin protocol")

// stream is the stream of a plugin inside DMRHub, as the plugin service
// serves it.
type stream struct {
	i   *Instance
	ctx context.Context //nolint:containedctx // the context of the stream
}

// Stream returns the stream of the plugin for the plugin service. Give it
// a context from plugins.InProcess.
func (i *Instance) Stream(ctx context.Context) pluginv1.PluginService_ConnectServer {
	return &stream{i: i, ctx: ctx}
}

// Send sends m to the plugin.
func (s *stream) Send(m *pluginv1.ConnectResponse) error {
	return s.i.write(m)
}

// Recv returns the next message of the plugin, and io.EOF when it ended.
func (s *stream) Recv() (*pluginv1.ConnectRequest, error) {
	select {
	case m, ok := <-s.i.frames:
		if !ok {
			return nil, io.EOF
		}
		return m, nil
	case <-s.ctx.Done():
		return nil, fmt.Errorf("the stream ended: %w", s.ctx.Err())
	}
}

// Context returns the context of the stream.
func (s *stream) Context() context.Context { return s.ctx }

// SetHeader does nothing, as a plugin inside DMRHub has no headers.
func (s *stream) SetHeader(metadata.MD) error { return nil }

// SendHeader does nothing, as a plugin inside DMRHub has no headers.
func (s *stream) SendHeader(metadata.MD) error { return nil }

// SetTrailer does nothing, as a plugin inside DMRHub has no trailers.
func (s *stream) SetTrailer(metadata.MD) {}

// SendMsg sends m, a ConnectResponse.
func (s *stream) SendMsg(m any) error {
	r, ok := m.(*pluginv1.ConnectResponse)
	if !ok {
		return errNotAMessage
	}
	return s.Send(r)
}

// RecvMsg receives into m, a ConnectRequest.
func (s *stream) RecvMsg(m any) error {
	r, ok := m.(*pluginv1.ConnectRequest)
	if !ok {
		return errNotAMessage
	}
	got, err := s.Recv()
	if err != nil {
		return err
	}
	r.Reset()
	r.Message = got.GetMessage()
	return nil
}
