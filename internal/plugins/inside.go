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

package plugins

import (
	"context"
	"errors"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

type insideKey struct{}

// InProcess returns ctx for the stream of the plugin id that runs inside
// DMRHub, which Connect takes without a token. Only the hub makes such a
// stream: a plugin that connects to the listener cannot.
func InProcess(ctx context.Context, id uint) context.Context {
	return context.WithValue(ctx, insideKey{}, id)
}

func insideOf(ctx context.Context) (uint, bool) {
	id, ok := ctx.Value(insideKey{}).(uint)
	return id, ok
}

// inside returns the plugin id that runs inside DMRHub, when it is on.
func (s *Service) inside(id uint) (models.Plugin, error) {
	p, err := models.FindPlugin(s.db, id)
	switch {
	case errors.Is(err, models.ErrPluginNotFound):
		return models.Plugin{}, errUnknownToken
	case err != nil:
		slog.Error("Plugins: cannot read a plugin", "plugin", id, "error", err)
		return models.Plugin{}, errTryLater
	case p.Kind == models.PluginExternal:
		return models.Plugin{}, errUnknownToken
	case !p.Enabled:
		return models.Plugin{}, errOff
	}
	return p, nil
}

// hello records the version that a plugin that runs as its own program
// says it has. A plugin inside DMRHub has the version of its module.
func (s *Service) hello(p *models.Plugin, h *pluginv1.Hello) {
	if p.Kind != models.PluginExternal || h.GetVersion() == p.Version {
		return
	}
	if err := models.NotePluginVersion(s.db, p.ID, h.GetVersion()); err != nil {
		slog.Warn("Plugins: cannot note a version", "plugin", p.Name, "error", err)
		return
	}
	p.Version = h.GetVersion()
}
