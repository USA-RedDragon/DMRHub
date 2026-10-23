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

package websocket

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	userTopicPrefix = "user:changed:"
	userWatchKey    = "websocket.userWatch"
)

// UserTopic is the pubsub topic that tells every replica that the user
// userID changed.
func UserTopic(userID uint) string {
	return userTopicPrefix + strconv.FormatUint(uint64(userID), 10)
}

// PublishUserChanged closes the websockets of the user userID on every
// replica. Call it after a change to the user's role, suspension, approval
// or existence; the clients reconnect and are checked again.
func PublishUserChanged(ps pubsub.PubSub, userID uint) error {
	if err := ps.Publish(UserTopic(userID), []byte(strconv.FormatUint(uint64(userID), 10))); err != nil {
		return fmt.Errorf("failed to publish the change of user %d: %w", userID, err)
	}
	return nil
}

// WatchUser subscribes a websocket request of a logged-in user to its
// UserTopic before the login checks run, so that no change after those
// checks is missed. The handler of CreateHandler closes the connection on
// the first message.
func WatchUser(ps pubsub.PubSub) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := sessions.Default(c).Get("user_id").(uint)
		if !ok {
			return
		}
		subscription := ps.Subscribe(UserTopic(userID))
		c.Set(userWatchKey, subscription)
		c.Next()
		if err := subscription.Close(); err != nil {
			slog.Error("Failed to close the user subscription of a websocket", "error", err)
		}
	}
}

func watchUser(ctx context.Context, cancel context.CancelCauseFunc, subscription pubsub.Subscription) {
	channel := subscription.Channel()
	select {
	case <-ctx.Done():
	case _, ok := <-channel:
		if ok {
			cancel(errUserChanged)
		}
	}
}
