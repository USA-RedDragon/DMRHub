// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build !tinygo

package plugin

import (
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/protobuf/proto"
)

// Message is a message of the plugin protocol.
type Message = proto.Message

func marshal(m Message) ([]byte, error) {
	return proto.Marshal(m) //nolint:wrapcheck // the callers wrap it
}

func unmarshal(b []byte, m Message) error {
	return proto.Unmarshal(b, m) //nolint:wrapcheck // the callers wrap it
}

func cloneHello(h *pluginv1.Hello) *pluginv1.Hello {
	return proto.CloneOf(h)
}
