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

package cmd

import (
	"context"
	"log/slog"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
)

func TestSetupTracing_EmptyEndpoint_ReturnsNoopCleanup(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Metrics.OTLPEndpoint = ""

	cleanup, err := setupTracing(cfg)
	if err != nil {
		t.Fatalf("expected no error for empty OTLP endpoint, got: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil no-op cleanup function for empty OTLP endpoint")
	}
	// The no-op cleanup should succeed without error.
	if err := cleanup(t.Context()); err != nil {
		t.Fatalf("expected no-op cleanup to return nil error, got: %v", err)
	}
}

func TestInitTracer_ValidEndpoint_ReturnsCleanup(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Metrics.OTLPEndpoint = "localhost:4317"

	// gRPC connections are lazy, so a well-formed endpoint won't fail at
	// creation time. Verify that initTracer returns a non-nil cleanup
	// and no error.
	cleanup, err := initTracer(cfg)
	if err != nil {
		t.Fatalf("expected no error for well-formed endpoint, got: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup function for well-formed endpoint")
	}
}

func TestSetupTracing_WithEndpoint_ReturnsCleanupAndNoError(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Metrics.OTLPEndpoint = "localhost:4317"

	cleanup, err := setupTracing(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup function when OTLP endpoint is set")
	}
}

func TestLiveConfigAppliesTheLogLevel(t *testing.T) { //nolint:paralleltest // sets the default logger of the process
	defer slog.SetDefault(slog.Default())
	l := liveConfig{dmr: manager.New(manager.Deps{})}

	l.Apply(&config.Config{LogLevel: config.LogLevelDebug})
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug logs are off after a change of the log level to debug")
	}
	l.Apply(&config.Config{LogLevel: config.LogLevelError})
	if slog.Default().Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("warnings are on after a change of the log level to error")
	}
}
