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
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/http/setupwizard"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/pkg/browser"
	"gorm.io/gorm"
)

const wizardStopTimeout = 10 * time.Second

// wizard is the setup wizard while it runs. It keeps one server, and so
// one link, from the database phase to the end.
type wizard struct {
	state  *setupwizard.State
	server http.Server
	done   chan any
	sig    chan os.Signal
}

func startWizard(cfg *config.Config, version, commit string) (*wizard, error) {
	token, err := utils.RandomPassword(12, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to make the token of the setup wizard: %w", err)
	}
	w := &wizard{state: setupwizard.NewState(), done: make(chan any, 1), sig: make(chan os.Signal, 1)}
	signal.Notify(w.sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	w.server = http.MakeSetupWizardServer(cfg, w.state, token, w.done, version, commit)
	if err := w.server.Start(); err != nil && !strings.Contains(err.Error(), "server closed") {
		slog.Error("failed to start HTTP server", "error", err)
	}
	url := "http://localhost:3005/setup?token=" + token
	slog.Info("Setup can be completed in a web browser")
	slog.Info("Opening setup wizard at " + url)
	if err := browser.OpenURL(url); err != nil {
		slog.Error("Failed to open browser, please open "+url+" manually", "error", err)
	}
	return w, nil
}

// waitForDatabase runs the database phase until the admin fixes the
// database setting. exit is true when a signal comes first.
func (w *wizard) waitForDatabase(fix *liveconfig.DatabaseFix) (liveconfig.Fixed, bool) {
	w.state.SetDatabaseFix(fix)
	select {
	case f := <-fix.Fixed():
		slog.Info("The setup wizard reached the database", "file", fix.State.Path)
		return f, false
	case sig := <-w.sig:
		slog.Error("Shutting down due to signal", "signal", sig)
		return liveconfig.Fixed{}, true
	}
}

// waitForSetup runs the setup phase until the admin completes it. It
// returns true when a signal comes first.
func (w *wizard) waitForSetup(live *liveconfig.Live, database *gorm.DB) bool {
	w.state.SetLive(live, database)
	select {
	case <-w.done:
		slog.Info("Setup complete, shutting down setup wizard")
		return false
	case sig := <-w.sig:
		slog.Error("Shutting down due to signal", "signal", sig)
		return true
	}
}

func (w *wizard) stop(ctx context.Context) {
	signal.Stop(w.sig)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), wizardStopTimeout)
	defer cancel()
	w.server.Stop(ctx)
}
