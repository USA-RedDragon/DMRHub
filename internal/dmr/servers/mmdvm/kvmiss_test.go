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

package mmdvm

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, _ := b.buf.Write(p)
	return n, nil
}

func (b *lockedLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestDMRDAfterTheSessionEndedLogsNoError plays the replica failover of the
// chaos stress run: the session of a repeater ends, which deletes its KV
// record, and the repeater sends voice before it logs in again. Each of
// those packets looks the record up and finds none, which is no failure.
//
//nolint:paralleltest // it swaps the default logger
func TestDMRDAfterTheSessionEndedLogsNoError(t *testing.T) {
	r := newDialectRig(t, 319700)
	r.login(dmrd.DialectMMDVM, 319700)
	r.s.closeSessions(context.Background())
	r.s.records.Wait()
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTCL)
	_, err := r.kv.GetRepeater(context.Background(), 319700)
	require.Error(t, err, "the end of the session must delete the record")

	var out lockedLog
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	defer slog.SetDefault(prev)
	for range 4 {
		r.send((&models.Packet{Signature: "DMRD", Repeater: 319700, Dst: 50000, GroupCall: true}).Encode())
	}
	assert.NotContains(t, out.String(), "level=ERROR", out.String())
	assert.NotContains(t, out.String(), "level=WARN", out.String())
}
