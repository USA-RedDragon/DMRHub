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

//go:build stress

package stress_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/retention"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestDatabase stores messages under load, runs the retention purge over
// a large backlog while the traffic flows, and checks the 250 caps of the
// inbox and the outbox while new messages arrive.
func TestDatabase(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "database")
	r := newRig(t, s, res, logs, rigOptions{})
	w := r.web(t)
	db := quiet(r.stack.DB)
	capID := uint(accountBase + 1)
	jar := w.account(t, r, capID)
	var user models.User
	require.NoError(t, db.First(&user, capID).Error)
	require.NoError(t, models.SetSMSStorage(db, &user, true, time.Now().Add(-time.Minute)))

	backlog := envInt("DMRHUB_STRESS_BACKLOG", 20000)
	old := time.Now().Add(-30 * 24 * time.Hour)
	seedBacklog(t, db, backlog, old)
	res.set("backlog_rows", float64(backlog))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := append(loadMix(), struct {
		weight int
		make   func(r *rig, rnd *rand.Rand, i int, slot2 bool) script
	}{25, func(r *rig, _ *rand.Rand, i int, _ bool) script {
		if i%2 == 0 {
			return smsScript("sms-to-cap", false, capID, userID(i), nil, sms.FormatIP, enums.DataTypeRate12)
		}
		return smsScript("sms-from-cap", false, userID(i), capID, r.privateWant(rptID(i)), sms.FormatIP, enums.DataTypeRate12)
	}})
	r.runLanes(ctx, s.Duration, m, s.Duty, 0, nil)

	time.Sleep(s.Duration / 3)
	r.stack.Config.DMR.Retention = config.Retention{Messages: 7, Transmissions: 7, Bursts: 7}
	p := retention.NewPurger(r.stack.Config, r.stack.DB, r.stack.KV)
	t0 := time.Now()
	out, err := p.Run(ctx, time.Now())
	purge := time.Since(t0)
	res.set("purge_s", purge.Seconds())
	if err != nil {
		res.fail(t, "the retention purge failed during traffic: %v", err)
	}
	res.set("purged_messages", float64(out.Messages))
	res.set("purged_transmissions", float64(out.Transmissions))
	var left int64
	db.Model(&models.Message{}).Where("received_at < ?", time.Now().Add(-7*24*time.Hour)).Count(&left)
	res.set("old_messages_left", float64(left))
	if left > 0 {
		res.fail(t, "%d messages older than the retention are left after the purge", left)
	}

	r.waitLanes()
	time.Sleep(6 * time.Second)
	r.led.finish(0, true)
	r.checkDelivery()
	var stored int64
	db.Model(&models.Message{}).Where("received_at > ?", time.Now().Add(-time.Hour)).Count(&stored)
	res.set("messages_stored", float64(stored))

	for _, box := range []string{"inbox", "outbox"} {
		var all int64
		col := "destination_id"
		if box == "outbox" {
			col = "source_id"
		}
		db.Model(&models.Message{}).Where(col+" = ?", capID).Count(&all)
		total, rows, status := messagesPage(t, w, jar, box, 1, 1000)
		res.set(box+"_rows_in_db", float64(all))
		res.set(box+"_total", float64(total))
		res.set(box+"_status", float64(status))
		if total > 250 || rows > 250 {
			res.fail(t, "%s returns total %d and %d rows, over the cap of 250", box, total, rows)
		}
		if all >= 250 && total != 250 {
			res.fail(t, "%s has %d messages in the database but total %d, not 250", box, all, total)
		}
	}
	res.set("sqlite_busy_logged", float64(logs.count(levelError, "CallTracker: error creating call")+logs.count(levelError, "CallTracker: error saving call")))
}

// seedBacklog writes n old transmissions, each with a message and its raw
// bursts, as a hub that ran for a month has.
func seedBacklog(t *testing.T, db *gorm.DB, n int, at time.Time) {
	t.Helper()
	const batch = 500
	raw := make([]byte, 33*20)
	for i := 0; i < n; i += batch {
		calls := make([]models.Call, 0, batch)
		for k := i; k < min(i+batch, n); k++ {
			calls = append(calls, models.Call{
				StreamID: uint(0x7000000 + k), StartTime: at, Kind: models.CallKindData, SubKind: "sms", //nolint:gosec
				SourceID: userID(k % 10), DestinationID: accountBase + 1, CallData: raw, Transport: "mmdvm",
			})
		}
		require.NoError(t, db.CreateInBatches(calls, batch).Error)
		msgs := make([]models.Message, 0, len(calls))
		text := "OLD"
		for k := range calls {
			id := calls[k].ID
			msgs = append(msgs, models.Message{
				ReceivedAt: at, TransmissionID: &id, SourceID: calls[k].SourceID, DestinationID: calls[k].DestinationID,
				Format: "ip", Text: &text, ChecksumOK: true, Raw: raw[:40], StreamID: calls[k].StreamID,
			})
		}
		require.NoError(t, db.CreateInBatches(msgs, batch).Error)
	}
}

func messagesPage(t *testing.T, w *web, jar testutils.CookieJar, box string, page, limit int) (int, int, int) {
	t.Helper()
	rec := testutils.Do(t, w.router, jar, http.MethodGet, fmt.Sprintf("/api/v1/messages/%s?page=%d&limit=%d", box, page, limit), nil)
	var body struct {
		Total    int               `json:"total"`
		Messages []json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Total, len(body.Messages), rec.Code
}
