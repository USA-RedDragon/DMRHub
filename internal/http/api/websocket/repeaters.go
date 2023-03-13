// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

package websocket

import (
	"context"
	"net/http"

	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type RepeatersWebsocket struct {
	websocket.Websocket
	redis *redis.Client
	db    *gorm.DB
}

func CreateRepeatersWebsocket(db *gorm.DB, redis *redis.Client) *RepeatersWebsocket {
	return &RepeatersWebsocket{
		redis: redis,
		db:    db,
	}
}

func (c *RepeatersWebsocket) OnMessage(ctx context.Context, r *http.Request, w websocket.Writer, _ sessions.Session, msg []byte, t int) {
}

func (c *RepeatersWebsocket) OnConnect(ctx context.Context, r *http.Request, w websocket.Writer, session sessions.Session) {
}

func (c *RepeatersWebsocket) OnDisconnect(ctx context.Context, r *http.Request, _ sessions.Session) {
}
