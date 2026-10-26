// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build wasip1 && tinygo

package plugin

import "syscall"

func setNonblock(fd int) error {
	flags, err := syscall.Fcntl(fd, syscall.F_GETFL, 0)
	if err != nil {
		return err //nolint:wrapcheck // the caller says what failed
	}
	_, err = syscall.Fcntl(fd, syscall.F_SETFL, flags|syscall.O_NONBLOCK)
	return err //nolint:wrapcheck // the caller says what failed
}
