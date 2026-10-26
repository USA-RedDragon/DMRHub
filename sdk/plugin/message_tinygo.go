// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build tinygo

package plugin

import (
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// Message is a message of the plugin protocol. A plugin built with TinyGo
// encodes its messages with the code that protoc-gen-go-lite generates,
// without reflection, in the same bytes as google.golang.org/protobuf.
type Message interface {
	MarshalVT() ([]byte, error)
	UnmarshalVT(b []byte) error
}

func marshal(m Message) ([]byte, error) {
	return m.MarshalVT() //nolint:wrapcheck // the callers wrap it
}

func unmarshal(b []byte, m Message) error {
	return m.UnmarshalVT(b) //nolint:wrapcheck // the callers wrap it
}

func cloneHello(h *pluginv1.Hello) *pluginv1.Hello {
	return h.CloneVT()
}
