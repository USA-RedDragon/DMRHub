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

package testutils

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
)

// Fronts are the UDPFronts of the protocols of a test. A nil front is a
// protocol that the test does not put behind one.
type Fronts struct {
	MMDVM        *UDPFront
	IPSC         *UDPFront
	OpenBridge   *UDPFront
	WebTransport *UDPFront
}

// Add adds the servers of r to the fronts.
func (f Fronts) Add(t *testing.T, r *Replica) {
	t.Helper()
	f.each(r, func(front *UDPFront, addr string) { front.Add(t, addr) })
}

func (f Fronts) each(r *Replica, fn func(*UDPFront, string)) {
	for _, x := range []struct {
		front *UDPFront
		addr  string
	}{{f.MMDVM, r.MMDVMAddr}, {f.IPSC, r.IPSCAddr}, {f.OpenBridge, r.OpenBridgeAddr}, {f.WebTransport, r.WebTransportAddr}} {
		if x.front != nil {
			fn(x.front, x.addr)
		}
	}
}

// Terminate stops r as a rolling restart stops a pod of DMRHub: r marks
// itself as draining, as DMRHub does at once at SIGTERM; the fronts drain
// it, as Envoy Gateway does for a pod that terminates, which takes longer;
// r drains for at most timeout and stops with a handoff, as when other
// replicas run; then the fronts remove it, as the pod is gone. reg is the
// registry of r, from Register.
func (r *Replica) Terminate(t *testing.T, reg *servers.InstanceRegistry, f Fronts, timeout time.Duration) {
	t.Helper()
	ctx := context.Background()
	reg.MarkDraining(ctx)
	f.each(r, func(front *UDPFront, addr string) { front.Drain(addr) })
	manager.New(manager.Deps{Hub: r.Hub}).Drain(ctx, reg, timeout)
	r.Hub.StartDraining()
	r.Hub.WaitForCalls()
	graceful := reg.OtherInstancesExist(ctx)
	reg.Deregister(ctx)
	stop := servers.WithGracefulHandoff(ctx, graceful)
	for _, s := range []interface{ Stop(context.Context) error }{r.MMDVMServer, r.IPSCServer, r.OpenBridgeServer} {
		if err := s.Stop(stop); err != nil {
			t.Errorf("stopping a server of the replica: %v", err)
		}
	}
	r.Hub.Stop(ctx)
	r.CallTracker.Stop()
	f.each(r, func(front *UDPFront, addr string) { front.Remove(addr) })
}
