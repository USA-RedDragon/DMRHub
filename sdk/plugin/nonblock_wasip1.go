// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build wasip1 && !tinygo

package plugin

import "syscall"

func setNonblock(fd int) error {
	return syscall.SetNonblock(fd, true) //nolint:wrapcheck // the caller says what failed
}
