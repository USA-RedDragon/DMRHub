// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin

import (
	"runtime/debug"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// helloOf returns a copy of hello whose version, when hello has none, is
// the version of the plugin's Go module.
func helloOf(hello *pluginv1.Hello) *pluginv1.Hello {
	out := &pluginv1.Hello{}
	if hello != nil {
		out = cloneHello(hello)
	}
	if out.GetVersion() == "" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "(devel)" {
			out.Version = bi.Main.Version
		}
	}
	return out
}
