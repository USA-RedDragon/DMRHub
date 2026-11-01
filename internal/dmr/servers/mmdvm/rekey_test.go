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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repeaterRecordPrefix starts the KV key of the record of each repeater.
const repeaterRecordPrefix = "mmdvm:repeater:"

// heldGetKV holds the next Get of a repeater record after hold is set
// until release closes.
type heldGetKV struct {
	kv.KV
	hold    atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (h *heldGetKV) Get(ctx context.Context, key string) ([]byte, error) {
	if strings.HasPrefix(key, repeaterRecordPrefix) && h.hold.CompareAndSwap(true, false) {
		close(h.entered)
		<-h.release
	}
	return h.KV.Get(ctx, key) //nolint:wrapcheck
}

// noPong fails the test when s answered a ping of the repeater id at the
// peer, or holds a session of it, once s handled the packets that the rig
// gave it.
func (r *dialectRig) noPong(s *Server, id uint32, msg string) {
	r.t.Helper()
	r.handled(s)
	marker := []byte("marker")
	_, err := s.Server.WriteToUDP(marker, &r.from)
	require.NoError(r.t, err)
	for {
		p := r.next("marker")
		if bytes.Equal(p, marker) {
			break
		}
		k, err := dmrd.DecodeControl(p, dmrd.DialectMMDVM)
		require.NoError(r.t, err)
		assert.NotEqual(r.t, dmrd.CommandMSTPONG, k.Command, msg)
	}
	assert.False(r.t, serves(s, id), msg)
}

// TestAReplicaDoesNotResumeASessionThatLoggedInWithAnOldPassword logs a
// hotspot in to one replica and then changes its password while the KV
// record of that session is still there. Another replica must not resume
// the session from the record, for a ping or for egress.
func TestAReplicaDoesNotResumeASessionThatLoggedInWithAnOldPassword(t *testing.T) {
	t.Parallel()
	const id = 3000201
	r := newDialectRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	second := r.replica(r.store)

	require.NoError(t, models.SetRepeaterPassword(r.s.DB, id, "n3wPassw0rd9"))
	second.endSessions(context.Background(), id, connlog.PasswordRotated)

	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	r.sendTo(second, p)
	r.noPong(second, id, "a session that logged in with the old password resumed")
	_, ok := second.resumeForEgress(context.Background(), id)
	assert.False(t, ok, "egress resumed a session that logged in with the old password")
}

// TestAResumeThatReadTheRecordBeforeAPasswordChangeDoesNotTakeTheSession
// holds the read of the KV record by a ping to another replica until the
// password of the hotspot changes there, as when the replica read the
// record and the password before the change was saved.
func TestAResumeThatReadTheRecordBeforeAPasswordChangeDoesNotTakeTheSession(t *testing.T) {
	t.Parallel()
	const id = 3000202
	r := newDialectRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	held := &heldGetKV{KV: r.store, entered: make(chan struct{}), release: make(chan struct{})}
	second := r.replica(held)

	held.hold.Store(true)
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	r.sendTo(second, p)
	<-held.entered
	second.endSessions(context.Background(), id, connlog.PasswordRotated)
	close(held.release)
	r.noPong(second, id, "a session that logged in with the old password resumed")
}

// TestAReplicaDoesNotResumeASessionWithoutASecretCheck logs a hotspot in
// and then drops the secret check from the KV record of the session, as in a
// record that an older DMRHub wrote. Another replica must not resume the
// session from it, for a ping or for egress, so the hotspot logs in again.
func TestAReplicaDoesNotResumeASessionWithoutASecretCheck(t *testing.T) {
	t.Parallel()
	const id = 3000203
	r := newDialectRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	rec, err := r.kv.GetRepeater(context.Background(), id)
	require.NoError(t, err)
	require.NotEmpty(t, rec.SecretCheck)
	rec.SecretCheck = nil
	r.kv.StoreRepeater(context.Background(), id, rec)
	second := r.replica(r.store)

	_, ok := second.resumeForEgress(context.Background(), id)
	assert.False(t, ok, "egress resumed a session without a secret check")
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	r.sendTo(second, p)
	r.noPong(second, id, "a session without a secret check resumed")
}
