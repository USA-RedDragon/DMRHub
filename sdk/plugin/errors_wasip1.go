// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build wasip1

package plugin

import "errors"

// ErrNoWelcome is returned when the hub does not start with a Welcome.
var ErrNoWelcome = errors.New("the hub did not welcome the plugin")
