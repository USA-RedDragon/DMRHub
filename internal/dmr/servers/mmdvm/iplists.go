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
	"context"
	"log/slog"
	"net"
	"net/netip"

	"github.com/USA-RedDragon/DMRHub/internal/ipfilter"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
)

func (s *Server) ipList() *ipfilter.List {
	if s.hub == nil {
		return nil
	}
	return s.hub.MMDVMIPs()
}

func (s *Server) webTransportIPList() *ipfilter.List {
	if s.hub == nil {
		return nil
	}
	return s.hub.WebTransportIPs()
}

// listOf returns the IP list of the transport of ep.
func (s *Server) listOf(ep endpoint) *ipfilter.List {
	if ep.transport == webTransportName {
		return s.webTransportIPList()
	}
	return s.ipList()
}

// allows reports whether the IP list of the MMDVM server accepts the
// source address remote.
func (s *Server) allows(remote net.Addr) bool {
	l := s.ipList()
	return !l.Active() || l.Allows(ipfilter.AddrOf(remote))
}

// refuse drops the packet p from remote, which the IP list refuses. It
// answers a login, a config or a keepalive with MSTNAK, as the master
// answers a repeater that it does not take, and drops the other packets
// without an answer.
func (s *Server) refuse(t transport, remote net.Addr, p []byte) {
	metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonIPList)
	s.refusals.Refused("mmdvm", ipfilter.AddrOf(remote))
	for _, d := range [...]dmrd.Dialect{dmrd.DialectMMDVM, dmrd.DialectHBRP} {
		if !naked(dmrd.Classify(p, d)) {
			continue
		}
		id, ok := dmrd.RepeaterID(p, d)
		if !ok {
			return
		}
		var buf [16]byte
		nak := dmrd.Control{Command: dmrd.CommandMSTNAK, ID: id}
		b, err := nak.AppendBinary(buf[:0], d)
		if err != nil {
			return
		}
		if _, err := t.WriteTo(b, remote); err != nil {
			slog.Debug("Error sending MSTNAK to a refused address", "remoteAddr", remote, "error", err)
		}
		return
	}
}

// naked reports whether the master answers c from a refused address with
// MSTNAK: a login, a config or a keepalive.
func naked(c dmrd.Command) bool {
	return c == dmrd.CommandRPTL || c == dmrd.CommandRPTK || c == dmrd.CommandRPTC || c == dmrd.CommandRPTPING || c == dmrd.CommandMSTPING
}

// sourceOf returns the IP address that the packets of ep come from: the
// address of a UDP endpoint, or the one of the connection of a WebTransport
// session. It returns the invalid address for a session that is gone.
func (s *Server) sourceOf(ep endpoint) netip.Addr {
	if ep.transport == webTransportName {
		if s.wt == nil {
			return netip.Addr{}
		}
		return s.wt.sourceOf(ep.addr)
	}
	return ipfilter.AddrOf(ep.addr)
}

// endRefused ends, after a change of the IP lists, the sessions of the
// addresses that the IP list of their transport now refuses with MSTCL,
// and then closes the WebTransport sessions of those addresses.
func (s *Server) endRefused(ctx context.Context) {
	if s.stopped.Load() || (!s.ipList().Active() && !s.webTransportIPList().Active()) {
		return
	}
	s.mu.Lock()
	now := s.now()
	for _, link := range s.links {
		if s.listOf(link.ep).Allows(s.sourceOf(link.ep)) {
			continue
		}
		if link.m.State() != dmrdsession.StateIdle {
			slog.Info("Ending the session of a repeater whose address the IP list now refuses", "repeaterID", link.m.ID(), "remoteAddr", link.ep.String())
			var buf [dmrdsession.MaxOutputs]dmrdsession.Output
			s.run(ctx, now, link, link.m.Close(now, buf[:0]))
		}
		s.forget(link)
	}
	s.mu.Unlock()
	if l := s.webTransportIPList(); s.wt != nil && l.Active() {
		s.wt.endRefused(l)
	}
}
