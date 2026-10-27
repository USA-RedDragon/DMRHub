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

package modem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

const (
	requestAll    = "modem:request:all"
	requestPrefix = "modem:request:"
	replyPrefix   = "modem:reply:"
	kindPorts     = "ports"
	kindTest      = "test"
	idLen         = 16
	// TestTimeout bounds a test of a modem: the wait after the port
	// opens, the version tries and the two commands, with room to spare.
	TestTimeout = 20 * time.Second
	portsWait   = 2 * time.Second
	testWait    = TestTimeout + 5*time.Second
)

// ErrTestRuns is the error of a test of a modem while another test runs
// on the same replica.
var ErrTestRuns = errors.New("another test of the modem runs now")

// Host is the modem server of a replica.
type Host interface {
	// ModemStatus returns the live state of the modem server, and false
	// when it does not run.
	ModemStatus() (Status, bool)
}

// State is the live state of the modem, from the replica that drives it.
type State struct {
	// Status is nil when no replica runs the modem.
	Status *Status `json:"status"`
	// Self is whether the status is that of the replica that answers.
	Self bool `json:"self"`
}

// TestAnswer is the answer to a test of a modem.
type TestAnswer struct {
	// Replica is the replica that ran the test.
	Replica string `json:"replica,omitempty"`
	// Self is whether Replica is the replica that answers.
	Self bool `json:"self"`
	// Unanswered is true when Replica did not answer in time.
	Unanswered bool `json:"unanswered,omitempty"`
	// Live is true when a modem runs on the port, and the answer is its
	// live state instead of a test.
	Live   bool         `json:"live"`
	State  *State       `json:"state,omitempty"`
	Result *ProbeResult `json:"result,omitempty"`
}

// ReplicaPorts are the serial ports of one replica.
type ReplicaPorts struct {
	Replica string       `json:"replica"`
	Self    bool         `json:"self"`
	Ports   []SerialPort `json:"ports"`
}

// Replicas runs the modem requests of the Setup page on the replica that
// they are for: each replica lists its own serial ports and tests the
// ports of its own host, and asks the other replicas over PubSub.
type Replicas struct {
	// ID is the ID of this replica.
	ID     string
	PubSub pubsub.PubSub
	// KV holds the live states of the modems and the registry of the
	// replicas.
	KV   kv.KV
	Host Host
	// Probe is the options of a test; the zero value means
	// DefaultOptions.
	Probe Options
	// Ports lists the serial ports; nil means SerialPorts.
	Ports func() []SerialPort
	// PortsWait and TestWait bound the wait for the other replicas; zero
	// means 2 s and 25 s.
	PortsWait time.Duration
	TestWait  time.Duration

	testing sync.Mutex
	stop    context.CancelFunc
	wg      sync.WaitGroup
}

type request struct {
	ID    string        `json:"id"`
	From  string        `json:"from"`
	Kind  string        `json:"kind"`
	Modem *config.Modem `json:"modem,omitempty"`
}

type reply struct {
	Replica string       `json:"replica"`
	Ports   []SerialPort `json:"ports,omitempty"`
	Test    *TestAnswer  `json:"test,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// Start answers the requests of the other replicas until Stop.
func (r *Replicas) Start(ctx context.Context) {
	if r.PubSub == nil || r.ID == "" {
		return
	}
	ctx, r.stop = context.WithCancel(context.WithoutCancel(ctx))
	for _, topic := range []string{requestAll, requestPrefix + r.ID} {
		sub := r.PubSub.Subscribe(topic)
		r.wg.Add(1)
		go r.serve(ctx, sub)
	}
}

// Stop stops answering the other replicas.
func (r *Replicas) Stop() {
	if r.stop != nil {
		r.stop()
	}
	r.wg.Wait()
}

func (r *Replicas) serve(ctx context.Context, sub pubsub.Subscription) {
	defer r.wg.Done()
	defer func() { _ = sub.Close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-sub.Channel():
			if !ok {
				return
			}
			var req request
			if json.Unmarshal(b, &req) != nil || req.ID == "" || req.From == r.ID {
				continue
			}
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.answer(ctx, req)
			}()
		}
	}
}

func (r *Replicas) answer(ctx context.Context, req request) {
	out := reply{Replica: r.ID}
	switch req.Kind {
	case kindPorts:
		out.Ports = r.localPorts()
	case kindTest:
		if req.Modem == nil {
			return
		}
		a, err := r.localTest(ctx, *req.Modem)
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Test = &a
		}
	default:
		return
	}
	b, err := json.Marshal(out)
	if err != nil {
		return
	}
	if err := r.PubSub.Publish(replyPrefix+req.ID, b); err != nil {
		slog.Error("Could not answer a modem request of another replica", "replica", req.From, "error", err)
	}
}

func (r *Replicas) localPorts() []SerialPort {
	if r.Ports != nil {
		return r.Ports()
	}
	return SerialPorts()
}

// others returns the IDs of the other registered replicas.
func (r *Replicas) others(ctx context.Context) []string {
	if r.KV == nil || r.PubSub == nil {
		return nil
	}
	ids, err := servers.InstanceIDs(ctx, r.KV)
	if err != nil {
		slog.Error("Could not list the replicas", "error", err)
		return nil
	}
	return slices.DeleteFunc(ids, func(id string) bool { return id == r.ID })
}

// ask sends req to topic and returns the replies that come within wait,
// or sooner once every replica of want answered.
func (r *Replicas) ask(ctx context.Context, topic string, req request, want []string, wait time.Duration) []reply {
	id := make([]byte, idLen)
	_, _ = rand.Read(id)
	req.ID, req.From = hex.EncodeToString(id), r.ID
	b, err := json.Marshal(req)
	if err != nil {
		return nil
	}
	sub := r.PubSub.Subscribe(replyPrefix + req.ID)
	defer func() { _ = sub.Close() }()
	if err := r.PubSub.Publish(topic, b); err != nil {
		slog.Error("Could not ask the other replicas about the modem", "error", err)
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	var out []reply
	left := slices.Clone(want)
	for len(left) > 0 {
		select {
		case <-ctx.Done():
			return out
		case <-t.C:
			return out
		case b, ok := <-sub.Channel():
			if !ok {
				return out
			}
			var rep reply
			if json.Unmarshal(b, &rep) != nil || !slices.Contains(left, rep.Replica) {
				continue
			}
			out = append(out, rep)
			left = slices.DeleteFunc(left, func(id string) bool { return id == rep.Replica })
		}
	}
	return out
}

func waitOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// AllPorts returns the serial ports of this replica, then those of each
// other replica that answers in time.
func (r *Replicas) AllPorts(ctx context.Context) []ReplicaPorts {
	out := []ReplicaPorts{{Replica: r.ID, Self: true, Ports: r.localPorts()}}
	others := r.others(ctx)
	if len(others) == 0 {
		return out
	}
	for _, rep := range r.ask(ctx, requestAll, request{Kind: kindPorts}, others, waitOr(r.PortsWait, portsWait)) {
		ports := rep.Ports
		if ports == nil {
			ports = []SerialPort{}
		}
		out = append(out, ReplicaPorts{Replica: rep.Replica, Ports: ports})
	}
	slices.SortStableFunc(out[1:], func(a, b ReplicaPorts) int {
		switch {
		case a.Replica < b.Replica:
			return -1
		case a.Replica > b.Replica:
			return 1
		}
		return 0
	})
	return out
}

// State returns the live state of the modem: that of this replica when its
// link is up, else that of a replica whose link is up, else that of this
// replica when it runs the modem, else that of any replica that runs it.
func (r *Replicas) State(ctx context.Context) State {
	local, running := Status{}, false
	if r.Host != nil {
		local, running = r.Host.ModemStatus()
	}
	if running && local.Linked {
		return State{Status: &local, Self: true}
	}
	var down *Status
	for _, st := range Statuses(ctx, r.KV) {
		if st.Replica == r.ID {
			continue
		}
		if st.Linked {
			return State{Status: &st}
		}
		if down == nil {
			down = &st
		}
	}
	if running {
		return State{Status: &local, Self: true}
	}
	return State{Status: down}
}

func samePort(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// holder returns the replica whose modem holds port, and its live state:
// this replica when its modem server runs on port, or a replica whose
// link to a board on a port of that name is up.
func (r *Replicas) holder(ctx context.Context, port string) (string, *State) {
	if r.Host != nil {
		if st, ok := r.Host.ModemStatus(); ok && samePort(st.Port, port) {
			return r.ID, &State{Status: &st, Self: true}
		}
	}
	for _, st := range Statuses(ctx, r.KV) {
		if st.Replica != r.ID && st.Linked && st.Port == port {
			return st.Replica, &State{Status: &st}
		}
	}
	return "", nil
}

// localTest tests m on this replica, or returns the live state of the
// modem server of this replica when it runs on the port of m.
func (r *Replicas) localTest(ctx context.Context, m config.Modem) (TestAnswer, error) {
	a := TestAnswer{Replica: r.ID, Self: true}
	if r.Host != nil {
		if st, ok := r.Host.ModemStatus(); ok && samePort(st.Port, m.Port) {
			a.Live, a.State = true, &State{Status: &st, Self: true}
			return a, nil
		}
	}
	if !r.testing.TryLock() {
		return a, ErrTestRuns
	}
	defer r.testing.Unlock()
	opts := r.Probe
	if opts.Open == nil {
		opts = DefaultOptions()
	}
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()
	res := Probe(ctx, m, opts)
	a.Result = &res
	return a, nil
}

// owner returns the replica to test m on: replica when it is given, else
// one whose serial ports have the port of m, this one first, else this
// one.
func (r *Replicas) owner(ctx context.Context, m config.Modem, replica string) string {
	if replica != "" {
		return replica
	}
	for _, rp := range r.AllPorts(ctx) {
		for _, p := range rp.Ports {
			if p.Path == m.Port || p.Device == m.Port {
				return rp.Replica
			}
		}
	}
	return r.ID
}

// Test tests m on the replica of its port and returns the answer. When a
// modem holds the port, the answer is its live state and nothing opens
// the port. replica, when given, is the replica whose port the admin
// chose. A replica that does not answer in time gives an Unanswered
// answer.
func (r *Replicas) Test(ctx context.Context, m config.Modem, replica string) (TestAnswer, error) {
	if id, st := r.holder(ctx, m.Port); st != nil && (replica == "" || replica == id) {
		return TestAnswer{Replica: id, Self: st.Self, Live: true, State: st}, nil
	}
	target := r.owner(ctx, m, replica)
	if target == r.ID || r.PubSub == nil {
		return r.localTest(ctx, m)
	}
	replies := r.ask(ctx, requestPrefix+target, request{Kind: kindTest, Modem: &m}, []string{target}, waitOr(r.TestWait, testWait))
	if len(replies) == 0 {
		return TestAnswer{Replica: target, Unanswered: true}, nil
	}
	rep := replies[0]
	if rep.Error == ErrTestRuns.Error() {
		return TestAnswer{}, ErrTestRuns
	}
	if rep.Test == nil {
		return TestAnswer{Replica: target, Unanswered: true}, nil
	}
	a := *rep.Test
	a.Self = false
	if a.State != nil {
		a.State.Self = false
	}
	return a, nil
}
