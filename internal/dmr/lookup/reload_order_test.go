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

package lookup_test

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/stretchr/testify/assert"
)

// slowSource is a database whose answer is version, and whose next load,
// once held is set, reads the answer at once and returns it when release
// closes, as a load that a starved machine runs late.
type slowSource struct {
	version atomic.Int64
	held    atomic.Bool
	release chan struct{}
}

func (s *slowSource) load(int) (int64, error) {
	v := s.version.Load()
	if s.held.CompareAndSwap(true, false) {
		<-s.release
	}
	return v, nil
}

// TestAnOlderReloadDoesNotReplaceANewerOne reloads a key, as for one change,
// changes the answer, and reloads it again, as for the next change, while
// the first reload still runs. The first reload ends last, but the newer
// answer must stay.
func TestAnOlderReloadDoesNotReplaceANewerOne(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &slowSource{release: make(chan struct{})}
		c := lookup.New(clock.Real(), "test", time.Minute, src.load)
		_, _ = c.Get(1)
		src.held.Store(true)
		done := make(chan struct{})
		go func() {
			c.Reload(1)
			close(done)
		}()
		synctest.Wait()
		src.version.Store(1)
		c.Reload(1)
		close(src.release)
		<-done
		v, _ := c.Get(1)
		assert.Equal(t, int64(1), v, "an older reload replaced the answer of a newer one")
	})
}

// TestALoadBeforeAReloadIsNotKept changes the answer of a key while the
// first read of it loads it, and reloads the key for the change before the
// load ends. The load read the old answer, so the Cache must not keep it.
func TestALoadBeforeAReloadIsNotKept(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &slowSource{release: make(chan struct{})}
		c := lookup.New(clock.Real(), "test", time.Minute, src.load)
		src.held.Store(true)
		go func() { _, _ = c.Get(1) }()
		synctest.Wait()
		src.version.Store(1)
		c.Reload(1)
		close(src.release)
		synctest.Wait()
		v, _ := c.Get(1)
		assert.Equal(t, int64(1), v, "a load that began before a reload kept the old answer")
	})
}
