//go:build clockstep

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

package clockstep

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

// outDisk is the disk that hack/clockstep.sh reads the saved directory
// back from.
const outDisk = "/dev/vda"

// Main runs the tests of m. As the init of the VM of hack/clockstep.sh it
// first mounts /dev and /proc, brings up the loopback interface and writes
// /etc/hosts, and after the tests it writes the directory named by
// DMRHUB_CLOCK_STEP_OUT to the disk of the VM as a tar archive.
func Main(m *testing.M) int {
	if os.Getpid() != 1 {
		return m.Run()
	}
	if err := boot(); err != nil {
		fmt.Fprintln(os.Stderr, "clockstep: setting up the VM:", err)
		return 1
	}
	code := m.Run()
	if dir := os.Getenv("DMRHUB_CLOCK_STEP_OUT"); dir != "" {
		if err := save(dir, outDisk); err != nil {
			fmt.Fprintln(os.Stderr, "clockstep: saving", dir+":", err)
			code = 1
		}
	}
	return code
}

func boot() error {
	for _, m := range []struct{ fs, dir string }{{"devtmpfs", "/dev"}, {"proc", "/proc"}} {
		if err := os.MkdirAll(m.dir, 0o755); err != nil {
			return fmt.Errorf("making %s: %w", m.dir, err)
		}
		if err := syscall.Mount(m.fs, m.dir, m.fs, 0, ""); err != nil && !errors.Is(err, syscall.EBUSY) {
			return fmt.Errorf("mounting %s: %w", m.dir, err)
		}
	}
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return fmt.Errorf("making /etc: %w", err)
	}
	if err := os.WriteFile("/etc/hosts", []byte("127.0.0.1 localhost\n::1 localhost\n"), 0o644); err != nil { //nolint:gosec
		return fmt.Errorf("writing /etc/hosts: %w", err)
	}
	return loopbackUp()
}

func loopbackUp() error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("opening a socket: %w", err)
	}
	defer func() { _ = syscall.Close(fd) }()
	var ifr struct {
		name  [syscall.IFNAMSIZ]byte
		flags uint16
		_     [22]byte
	}
	copy(ifr.name[:], "lo")
	ifr.flags = syscall.IFF_UP | syscall.IFF_LOOPBACK | syscall.IFF_RUNNING
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&ifr))); errno != 0 { //nolint:gosec
		return fmt.Errorf("bringing up lo: %w", errno)
	}
	return nil
}

func save(dir, disk string) error {
	f, err := os.OpenFile(disk, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("opening %s: %w", disk, err)
	}
	tw := tar.NewWriter(f)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		return addFile(tw, dir, path)
	})
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", disk, err)
	}
	return nil
}

func addFile(tw *tar.Writer, dir, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	h, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("archiving %s: %w", path, err)
	}
	if h.Name, err = filepath.Rel(dir, path); err != nil {
		return fmt.Errorf("archiving %s: %w", path, err)
	}
	if err := tw.WriteHeader(h); err != nil {
		return fmt.Errorf("archiving %s: %w", path, err)
	}
	src, err := os.Open(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { _ = src.Close() }()
	if _, err := io.Copy(tw, src); err != nil {
		return fmt.Errorf("archiving %s: %w", path, err)
	}
	return nil
}
