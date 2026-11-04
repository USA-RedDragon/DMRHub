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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// answerWait and answerWindow are those of DMRHub's store and forward: it
// holds a confirmed private text that got no answer in answerWait, on a
// sweep that runs on a real-time timer, while the text is no older than
// answerWindow.
const (
	answerWait   = 10 * time.Second
	answerWindow = 2 * time.Minute
)

// expectHeld notes that the harness sent, at the time of the demo clock, a
// confirmed private text to radio that store and forward must hold.
func (r *runner) expectHeld(radio uint) {
	r.held[radio] = r.cursor
}

// waitHeldOfHub is waitHeld on the database of DMRHub.
func (r *runner) waitHeldOfHub(ctx context.Context) error {
	if len(r.held) == 0 {
		return nil
	}
	db, closeDB, err := demoDB(r)
	if err != nil {
		return err
	}
	defer closeDB()
	return r.waitHeld(ctx, db)
}

// waitHeld waits until DMRHub held each text of expectHeld that is
// answerWait or more old on the demo clock. It fails when the clock moved
// past the answer window of such a text before DMRHub held it, as DMRHub
// then never holds it.
func (r *runner) waitHeld(ctx context.Context, db *gorm.DB) error {
	report := time.Now().Add(stuckReport)
	for radio, sent := range r.held {
		age := r.cursor.Sub(sent)
		if age < answerWait {
			continue
		}
		for {
			var n int64
			if err := db.Raw("SELECT count(*) FROM held_messages WHERE radio_id = ?", radio).Scan(&n).Error; err != nil {
				return fmt.Errorf("could not read the held texts: %w", err)
			}
			if n > 0 {
				delete(r.held, radio)
				break
			}
			if age > answerWindow {
				return fmt.Errorf("the demo clock moved %s past the text to radio %d, past the answer window of %s, before DMRHub held it", age, radio, answerWindow)
			}
			if time.Now().After(report) {
				slog.Warn("DMRHub has not held a text yet", "radio", radio)
				report = time.Now().Add(stuckReport)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("waiting for the held text to radio %d: %w", radio, ctx.Err())
			case <-time.After(requestStep):
			}
		}
	}
	return nil
}
