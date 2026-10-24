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

package plugin

import (
	"context"
	"errors"
	"flag"
	"slices"
	"testing"
	"time"
)

var errDown = errors.New("the hub is down")

// fakeRun runs with a dial that fails, taking the time that took(attempt)
// says, and returns the waits between the attempts. It stops after the
// attempts.
func fakeRun(attempts int, took func(int) time.Duration) []time.Duration {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := time.Unix(0, 0)
	n := 0
	var waits []time.Duration
	dial := func(context.Context) (*Conn, error) {
		clock = clock.Add(took(n))
		n++
		if n == attempts {
			cancel()
		}
		return nil, errDown
	}
	wait := func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		clock = clock.Add(d)
		return true
	}
	run(ctx, dial, func(context.Context, *Conn) error { return nil }, wait, func() time.Time { return clock })
	return waits
}

func TestRunWaitsLongerUpToItsLimit(t *testing.T) {
	t.Parallel()
	waits := fakeRun(8, func(int) time.Duration { return 0 })
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	if !slices.Equal(waits, want) {
		t.Fatalf("waits %v, want %v", waits, want)
	}
}

func TestRunWaitsAgainFromASecondAfterALongConnection(t *testing.T) {
	t.Parallel()
	waits := fakeRun(4, func(i int) time.Duration {
		if i == 2 {
			return time.Minute
		}
		return 0
	})
	want := []time.Duration{time.Second, 2 * time.Second, time.Second}
	if !slices.Equal(waits, want) {
		t.Fatalf("waits %v, want %v", waits, want)
	}
}

func TestRunStopsWhileItWaits(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	dials := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, func(context.Context) (*Conn, error) {
			dials++
			time.AfterFunc(50*time.Millisecond, cancel)
			return nil, errDown
		}, func(context.Context, *Conn) error { return nil }, sleep, time.Now)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
	if dials != 1 {
		t.Fatalf("dialed %d times, want 1", dials)
	}
}

func TestTheOptionsComeFromFlagsAndTheEnvironment(t *testing.T) {
	t.Parallel()
	env := map[string]string{"DMRHUB_PLUGIN_ADDRESS": "hub:3006", "DMRHUB_PLUGIN_TOKEN": "dmrhub_plg_x", "DMRHUB_PLUGIN_FINGERPRINT": "AA:BB"}
	o, err := optionsFrom(flag.NewFlagSet("p", flag.ContinueOnError), nil, func(k string) string { return env[k] })
	if err != nil || o != (Options{Address: "hub:3006", Token: "dmrhub_plg_x", Fingerprint: "AA:BB"}) {
		t.Fatalf("got %+v, %v", o, err)
	}
	o, err = optionsFrom(flag.NewFlagSet("p", flag.ContinueOnError), []string{"-address", "other:1", "-fingerprint", ""}, func(k string) string { return env[k] })
	if err != nil || o != (Options{Address: "other:1", Token: "dmrhub_plg_x"}) {
		t.Fatalf("flags win: got %+v, %v", o, err)
	}
	_, err = optionsFrom(flag.NewFlagSet("p", flag.ContinueOnError), []string{"-address", "hub:3006"}, func(string) string { return "" })
	if !errors.Is(err, errUsage) {
		t.Fatalf("no token: got %v", err)
	}
}
