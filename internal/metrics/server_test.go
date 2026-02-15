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

package metrics_test

import (
	"net"
	"strconv"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
)

func TestCreateMetricsServer_DisabledReturnsNil(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Metrics: config.Metrics{
			Enabled: false,
		},
	}
	err := metrics.CreateMetricsServer(cfg)
	if err != nil {
		t.Fatalf("expected nil error when metrics disabled, got: %v", err)
	}
}

func TestCreateMetricsServer_PortInUseReturnsError(t *testing.T) {
	t.Parallel()

	// Occupy a port so the metrics server can't bind to it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	cfg := &config.Config{
		Metrics: config.Metrics{
			Enabled: true,
			Bind:    "127.0.0.1",
			Port:    port,
		},
	}

	// Before the fix this would panic. Now it should return an error.
	err = metrics.CreateMetricsServer(cfg)
	if err == nil {
		t.Fatal("expected error when port is already in use, got nil")
	}

	// Verify the error message mentions the address.
	expectedAddr := "127.0.0.1:" + strconv.Itoa(port)
	if !containsString(err.Error(), expectedAddr) {
		t.Errorf("expected error to mention address %q, got: %v", expectedAddr, err)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
