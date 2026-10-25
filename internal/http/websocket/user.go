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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	userTopicPrefix = "user:changed:"
	userWatchKey    = "websocket.userWatch"

	reasonUserChanged     = "user changed"
	reasonLoggedOut       = "logged out"
	reasonPasswordChanged = "password changed"
	reasonAppRemoved      = "app removed"
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
	return publishUserEvent(ps, userID, userEvent{Reason: reasonUserChanged})
}

// PublishLogout closes the websockets of the session sessionID of the user
// userID on every replica.
func PublishLogout(ps pubsub.PubSub, userID uint, sessionID string) error {
	return publishUserEvent(ps, userID, userEvent{Reason: reasonLoggedOut, Only: sessionKey(sessionID)})
}

// PublishPasswordChanged closes the websockets of the user userID on every
// replica, except those of the session keepSessionID.
func PublishPasswordChanged(ps pubsub.PubSub, userID uint, keepSessionID string) error {
	return publishUserEvent(ps, userID, userEvent{Reason: reasonPasswordChanged, Except: sessionKey(keepSessionID)})
}

// PublishAppRemoved closes, on every replica, the websockets that an app
// of the user userID opened with a token of the grant grantID.
func PublishAppRemoved(ps pubsub.PubSub, userID uint, grantID string) error {
	return publishUserEvent(ps, userID, userEvent{Reason: reasonAppRemoved, Grant: grantID})
}

type userEvent struct {
	Reason string `json:"reason"`
	Only   string `json:"only,omitempty"`
	Except string `json:"except,omitempty"`
	Grant  string `json:"grant,omitempty"`
}

func publishUserEvent(ps pubsub.PubSub, userID uint, event userEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to encode the change of user %d: %w", userID, err)
	}
	if err := ps.Publish(UserTopic(userID), data); err != nil {
		return fmt.Errorf("failed to publish the change of user %d: %w", userID, err)
	}
	return nil
}

func sessionKey(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:16])
}

type userWatch struct {
	subscription pubsub.Subscription
	session      string
	grant        string
}

// WatchUser subscribes a websocket request of a logged-in user to its
// UserTopic before the login checks run, so that no change after those
// checks is missed. The handler of CreateHandler closes the connection on
// each message that concerns its session.
func WatchUser(ps pubsub.PubSub) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID, ok := session.Get("user_id").(uint)
		if !ok {
			return
		}
		subscription := ps.Subscribe(UserTopic(userID))
		watch := &userWatch{subscription: subscription, session: sessionKey(session.ID())}
		if g, ok := utils.Grant(c); ok {
			watch.grant = g.ID
		}
		c.Set(userWatchKey, watch)
		c.Next()
		if err := subscription.Close(); err != nil {
			slog.Error("Failed to close the user subscription of a websocket", "error", err)
		}
	}
}

func (w *userWatch) run(ctx context.Context, cancel context.CancelCauseFunc) {
	channel := w.subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-channel:
			if !ok {
				return
			}
			if cause, closes := w.closes(msg); closes {
				cancel(cause)
				return
			}
		}
	}
}

func (w *userWatch) closes(msg []byte) (error, bool) {
	var event userEvent
	if err := json.Unmarshal(msg, &event); err != nil {
		return errUserChanged, true
	}
	if event.Only != "" && event.Only != w.session {
		return nil, false
	}
	if event.Except != "" && event.Except == w.session {
		return nil, false
	}
	if event.Grant != "" && event.Grant != w.grant {
		return nil, false
	}
	switch event.Reason {
	case reasonLoggedOut, reasonPasswordChanged, reasonAppRemoved:
		return policyCloseError{reason: event.Reason}, true
	default:
		return errUserChanged, true
	}
}
