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

// Package liveconfig keeps the settings that admins save in Setup in the
// database and puts them into effect on every replica of DMRHub while it
// runs.
//
// A replica runs with the defaults, over them the saved Setup settings,
// over them the config file, the environment and the flags. A setting
// that the file, the environment or a flag sets is locked: it wins at each
// start, and Setup cannot change it. When the deployment stops setting it,
// the saved Setup value comes back, or the default when there is none.
// The secrets and the other settings that take effect at start only come
// from the file, the environment and the flags alone, and never reach the
// database.
//
// A save is checked, written to the database and then announced. Each
// replica puts the new settings into effect and reports how that went,
// both on the pubsub and in a record in the KV store, which also holds the
// values it runs with, so that Setup can show when replicas disagree.
package liveconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"gorm.io/gorm"
)

const (
	changedTopic  = "dmrhub:liveconfig:changed"
	reportsTopic  = "dmrhub:liveconfig:reports"
	recordPrefix  = "dmrhub:liveconfig:replica:"
	defaultEvery  = 10 * time.Second
	defaultWait   = 5 * time.Second
	recordTTLRuns = 3
	scanPage      = 100
)

var (
	// ErrLocked is a change in Setup of a setting that the config file,
	// the environment or a flag sets.
	ErrLocked = errors.New("this setting is set by the deployment, in the config file, the environment or a flag, and Setup cannot change it")
	// ErrStartOnly is a change in Setup of a setting that takes effect at
	// the start of DMRHub only.
	ErrStartOnly = errors.New("this setting takes effect at the start of DMRHub only; set it in the config file or the environment of the deployment")
)

// Applier puts a config into effect on a replica. For each part that
// cannot take its new settings, Apply sets the settings of that part in
// next back to the ones it runs with and returns why.
type Applier interface {
	Apply(next *config.Config) []ApplyError
}

// Options are what a Live is made with.
type Options struct {
	// Base is the config of the defaults, the config file, the
	// environment and the flags, with the Locks that they set.
	Base  *config.Config
	Locks Locks
	DB    *gorm.DB
	// Current holds the config that the replica runs with. Nil makes a
	// new one.
	Current *config.Current
	// LoadedFile is the config file that the replica loaded, empty for
	// none. LegacyFile is the file that DMRHub wrote its settings to
	// before it kept them in the database. Boot imports the Setup
	// settings of LoadedFile when it is LegacyFile; see Boot.
	LoadedFile string
	LegacyFile string
	// WizardDatabase is whether the database settings come from the file
	// in which the setup wizard keeps them; see DatabaseState.
	WizardDatabase bool
}

// Replica is what a Live needs to share the settings with the other
// replicas.
type Replica struct {
	ID      string
	PubSub  pubsub.PubSub
	KV      kv.KV
	Applier Applier
	// Every is how often the replica renews its record and looks for a
	// save that it missed. 0 is 10 seconds.
	Every time.Duration
	// Wait is how long a save waits for the reports of the other
	// replicas. 0 is 5 seconds.
	Wait time.Duration
}

// ApplyError is a server of a replica that could not take its new
// settings.
type ApplyError struct {
	Server string `json:"server"`
	// Running is whether the server still runs with its previous settings.
	Running bool   `json:"running"`
	Error   string `json:"error"`
}

// FieldError is a setting that a replica cannot run with.
type FieldError struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

// Report is how a replica put a revision of the saved settings into
// effect, with the values of the settings it runs with, without the
// secrets.
type Report struct {
	Replica  string    `json:"replica"`
	Revision uint64    `json:"revision"`
	At       time.Time `json:"at"`
	// ApplyErrors are the servers that could not take the new settings.
	ApplyErrors []ApplyError `json:"apply_errors"`
	// Invalid holds why the replica cannot run with the saved settings,
	// with its own locked settings. It then keeps its previous settings.
	Invalid []FieldError               `json:"invalid"`
	Values  map[string]json.RawMessage `json:"values"`
	Sources map[string]Source          `json:"sources"`
}

// Failed reports whether the replica could not put all of its revision
// into effect.
func (r Report) Failed() bool {
	return len(r.ApplyErrors) > 0 || len(r.Invalid) > 0
}

// Live keeps the Setup settings of one replica.
type Live struct {
	opts Options
	cur  *config.Current
	keys []Key

	mu        sync.Mutex
	saved     Saved
	report    Report
	rep       Replica
	generated map[string]string

	stopMu  sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

// New returns a Live that runs with opts.Base until Boot.
func New(opts Options) *Live {
	cur := opts.Current
	if cur == nil {
		cur = config.NewCurrent(opts.Base)
	}
	if opts.Locks == nil {
		opts.Locks = Locks{}
	}
	return &Live{opts: opts, cur: cur, keys: Keys(), saved: Saved{Values: map[string]json.RawMessage{}}}
}

// Current returns the holder of the config that the replica runs with.
func (l *Live) Current() *config.Current {
	return l.cur
}

// Keys returns the settings, with whether Setup changes them.
func (l *Live) Keys() []Key {
	return l.keys
}

// Locks returns the locked settings of the replica.
func (l *Live) Locks() Locks {
	return l.opts.Locks
}

// Editable reports whether Setup changes the setting k on this replica.
func (l *Live) Editable(k Key) bool {
	_, locked := l.opts.Locks[k.Path]
	return k.Setup && !locked
}

// Boot reads the saved settings and makes the config of the replica that
// of opts.Base with them, without putting it into effect; DMRHub starts
// with it.
//
// On the first start after DMRHub kept the Setup settings in the database,
// there are none yet. Boot then imports the Setup settings of the config
// file that the replica loaded, if that is the file that DMRHub used to
// write, so that the deployment can take them out of the file and keep
// their values. It imports from no other file: a file that DMRHub wrote
// but did not load, as when ./config.yaml exists, holds settings that the
// hub never ran with. It imports no setting that the environment or a flag
// overrides, and no value equal to its default.
func (l *Live) Boot(ctx context.Context) error {
	if err := l.importLegacy(ctx); err != nil {
		return err
	}
	if err := l.loadGenerated(ctx); err != nil {
		return err
	}
	saved, err := LoadSaved(ctx, l.opts.DB)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	next := l.effective(saved)
	l.saved = saved
	l.cur.Set(next)
	l.report = l.newReport(saved.Revision, nil, nil)
	return nil
}

func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, aerr := filepath.Abs(a)
	ba, berr := filepath.Abs(b)
	return aerr == nil && berr == nil && aa == ba
}

func (l *Live) importLegacy(ctx context.Context) error {
	values := map[string]json.RawMessage{}
	if sameFile(l.opts.LoadedFile, l.opts.LegacyFile) {
		defaults, err := configulator.New(config.ConfigSchema()).Default()
		if err != nil {
			return fmt.Errorf("failed to make the default config: %w", err)
		}
		for _, k := range l.keys {
			lock, ok := l.opts.Locks[k.Path]
			if !k.Setup || !ok || lock.Source != SourceFile || !sameFile(lock.File, l.opts.LoadedFile) || k.Equal(l.opts.Base, &defaults) {
				continue
			}
			v, err := k.Value(l.opts.Base)
			if err != nil {
				return err
			}
			values[k.Path] = v
		}
	}
	made, err := createRow(ctx, l.opts.DB, values)
	if err != nil {
		return err
	}
	if made && len(values) > 0 {
		paths := make([]string, 0, len(values))
		for p := range values {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		slog.Info("Imported the Setup settings of the config file that DMRHub used to write; take them out of the file to change them in Setup",
			"file", l.opts.LoadedFile, "settings", paths)
	}
	return nil
}

// effective returns the config of opts.Base with the saved values of the
// settings that Setup changes and the deployment does not lock.
func (l *Live) effective(saved Saved) *config.Config {
	next := *l.opts.Base
	for _, k := range l.keys {
		raw, ok := saved.Values[k.Path]
		if !ok || !l.Editable(k) {
			continue
		}
		if err := k.Set(&next, raw); err != nil {
			slog.Error("A saved Setup setting does not fit its setting, so the replica leaves it out", "setting", k.Path, "error", err)
		}
	}
	for _, k := range l.keys {
		if v, ok := l.generated[k.Path]; ok {
			k.field(&next).SetString(v)
		}
	}
	return &next
}

func (l *Live) source(k Key) Source {
	if lock, ok := l.opts.Locks[k.Path]; ok {
		return lock.Source
	}
	if _, ok := l.generated[k.Path]; ok {
		return SourceGenerated
	}
	if l.opts.WizardDatabase && under(k.Path, "database") {
		return SourceWizard
	}
	if _, ok := l.saved.Values[k.Path]; ok && k.Setup {
		return SourceSetup
	}
	return SourceDefault
}

func (l *Live) newReport(rev uint64, applyErrs []ApplyError, invalid []config.ValidationError) Report {
	r := Report{
		Replica: l.rep.ID, Revision: rev, At: time.Now(),
		ApplyErrors: []ApplyError{}, Invalid: []FieldError{},
		Values: map[string]json.RawMessage{}, Sources: map[string]Source{},
	}
	r.ApplyErrors = append(r.ApplyErrors, applyErrs...)
	for _, e := range invalid {
		r.Invalid = append(r.Invalid, FieldError{Field: e.Field, Error: e.Err.Error()})
	}
	running := l.cur.Get()
	for _, k := range l.keys {
		if k.Secret {
			continue
		}
		if v, err := k.Value(running); err == nil {
			r.Values[k.Path] = v
		}
		r.Sources[k.Path] = l.source(k)
	}
	return r
}

// Check returns why next cannot be saved: a change of a setting that Setup
// does not change on this replica, and the checks of the config that the
// replica would run with.
func (l *Live) Check(next *config.Config) []config.ValidationError {
	running := l.cur.Get()
	var errs []config.ValidationError
	candidate := *running
	for _, k := range l.keys {
		if l.Editable(k) {
			k.Copy(&candidate, next)
			continue
		}
		if k.Equal(next, running) {
			continue
		}
		err := ErrStartOnly
		if _, locked := l.opts.Locks[k.Path]; locked {
			err = ErrLocked
		}
		errs = append(errs, config.ValidationError{Field: k.Path, Err: err})
	}
	return append(errs, candidate.ValidateWithFields()...)
}

// SaveResult is how the replicas put a save into effect.
type SaveResult struct {
	Revision uint64
	// Replicas holds the report of this replica first, then those of the
	// other replicas, in the order of their IDs.
	Replicas []ReplicaResult
}

// ReplicaResult is the report of one replica on a save. Answered is false
// for a replica that did not report within the wait; its Report is then
// the last one it stored, if any.
type ReplicaResult struct {
	Report
	Self     bool
	Answered bool
}

// Failed reports whether a replica that answered could not put the save
// into effect.
func (r SaveResult) Failed() bool {
	for _, rep := range r.Replicas {
		if rep.Answered && rep.Failed() {
			return true
		}
	}
	return false
}

// Save checks next with Check, saves the values of the settings that Setup
// changes on this replica, puts them into effect here and announces them
// to the other replicas, and waits for their reports. A setting whose new
// value is its value in opts.Base is no longer saved, so that it follows
// the default. A locked setting keeps its saved value.
func (l *Live) Save(ctx context.Context, next *config.Config) (*SaveResult, []config.ValidationError, error) {
	if errs := l.Check(next); len(errs) > 0 {
		return nil, errs, nil
	}
	editable := make([]Key, 0, len(l.keys))
	for _, k := range l.keys {
		if l.Editable(k) {
			editable = append(editable, k)
		}
	}
	l.mu.Lock()
	rep := l.rep
	l.mu.Unlock()
	var sub pubsub.Subscription
	if rep.PubSub != nil {
		sub = rep.PubSub.Subscribe(reportsTopic)
		defer func() { _ = sub.Close() }()
	}
	var encodeErr error
	rev, err := store(ctx, l.opts.DB, func(values map[string]json.RawMessage) {
		for _, k := range editable {
			if k.Equal(next, l.opts.Base) {
				delete(values, k.Path)
				continue
			}
			v, err := k.Value(next)
			if err != nil {
				encodeErr = err
				return
			}
			values[k.Path] = v
		}
	})
	if err == nil {
		err = encodeErr
	}
	if err != nil {
		return nil, nil, err
	}
	self, err := l.Sync(ctx)
	if err != nil {
		return nil, nil, err
	}
	others := l.otherRecords(ctx)
	res := &SaveResult{Revision: rev, Replicas: []ReplicaResult{{Report: self, Self: true, Answered: true}}}
	if rep.PubSub == nil {
		return res, nil, nil
	}
	if b, err := json.Marshal(announcement{Revision: rev}); err == nil {
		if err := rep.PubSub.Publish(changedTopic, b); err != nil {
			slog.Error("Failed to announce the saved Setup settings; the other replicas take them at their next check", "error", err)
		}
	}
	res.Replicas = append(res.Replicas, l.await(ctx, sub, rev, others, rep.wait())...)
	return res, nil, nil
}

type announcement struct {
	Revision uint64 `json:"revision"`
}

func (r Replica) wait() time.Duration {
	if r.Wait > 0 {
		return r.Wait
	}
	return defaultWait
}

func (r Replica) every() time.Duration {
	if r.Every > 0 {
		return r.Every
	}
	return defaultEvery
}

// await collects the reports on rev of the replicas of others until each
// answered or wait passes.
func (l *Live) await(ctx context.Context, sub pubsub.Subscription, rev uint64, others map[string]Report, wait time.Duration) []ReplicaResult {
	answered := map[string]Report{}
	for id, r := range others {
		if r.Revision >= rev {
			answered[id] = r
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for len(answered) < len(others) {
		select {
		case <-ctx.Done():
			return results(others, answered)
		case <-timer.C:
			return results(others, answered)
		case b, ok := <-sub.Channel():
			if !ok {
				return results(others, answered)
			}
			var r Report
			if err := json.Unmarshal(b, &r); err != nil {
				continue
			}
			if _, ok := others[r.Replica]; ok && r.Revision >= rev {
				answered[r.Replica] = r
			}
		}
	}
	return results(others, answered)
}

func results(others, answered map[string]Report) []ReplicaResult {
	ids := make([]string, 0, len(others))
	for id := range others {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ReplicaResult, 0, len(ids))
	for _, id := range ids {
		if r, ok := answered[id]; ok {
			out = append(out, ReplicaResult{Report: r, Answered: true})
			continue
		}
		out = append(out, ReplicaResult{Report: others[id]})
	}
	return out
}

// Sync puts the latest saved settings into effect on this replica, if it
// does not run with them yet, and returns its report.
func (l *Live) Sync(ctx context.Context) (Report, error) {
	saved, err := LoadSaved(ctx, l.opts.DB)
	if err != nil {
		return Report{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if saved.Revision == l.report.Revision && l.report.At != (time.Time{}) {
		return l.report, nil
	}
	next := l.effective(saved)
	l.saved = saved
	if invalid := next.ValidateWithFields(); len(invalid) > 0 {
		slog.Error("This replica cannot run with the saved Setup settings and keeps its previous settings", "revision", saved.Revision, "errors", invalid)
		l.report = l.newReport(saved.Revision, nil, invalid)
		l.publish(ctx)
		return l.report, nil
	}
	var applyErrs []ApplyError
	if l.rep.Applier != nil {
		applyErrs = l.rep.Applier.Apply(next)
	}
	l.cur.Set(next)
	l.report = l.newReport(saved.Revision, applyErrs, nil)
	slog.Info("Running with the saved Setup settings", "revision", saved.Revision, "applyErrors", len(applyErrs))
	l.publish(ctx)
	return l.report, nil
}

// publish stores the report of the replica in its record and sends it to
// the replicas that wait for it. The caller holds l.mu.
func (l *Live) publish(ctx context.Context) {
	b, err := json.Marshal(l.report)
	if err != nil {
		slog.Error("Failed to encode the report of the Setup settings", "error", err)
		return
	}
	l.writeRecord(ctx, b)
	if l.rep.PubSub != nil {
		if err := l.rep.PubSub.Publish(reportsTopic, b); err != nil {
			slog.Error("Failed to send the report of the Setup settings", "error", err)
		}
	}
}

func (l *Live) writeRecord(ctx context.Context, b []byte) {
	if l.rep.KV == nil || l.rep.ID == "" {
		return
	}
	key := recordPrefix + l.rep.ID
	if err := l.rep.KV.Set(ctx, key, b); err != nil {
		slog.Error("Failed to store the record of the Setup settings of this replica", "error", err)
		return
	}
	if err := l.rep.KV.Expire(ctx, key, recordTTLRuns*l.rep.every()); err != nil {
		slog.Error("Failed to set the lifetime of the record of the Setup settings of this replica", "error", err)
	}
}

// records returns the stored reports of the live replicas, by ID.
func (l *Live) records(ctx context.Context) map[string]Report {
	l.mu.Lock()
	store := l.rep.KV
	l.mu.Unlock()
	out := map[string]Report{}
	if store == nil {
		return out
	}
	var cursor uint64
	for {
		keys, next, err := store.Scan(ctx, cursor, recordPrefix+"*", scanPage)
		if err != nil {
			slog.Error("Failed to list the replicas", "error", err)
			return out
		}
		for _, key := range keys {
			b, err := store.Get(ctx, key)
			if err != nil {
				continue
			}
			var r Report
			if err := json.Unmarshal(b, &r); err != nil || r.Replica == "" {
				continue
			}
			out[r.Replica] = r
		}
		if next == 0 {
			return out
		}
		cursor = next
	}
}

func (l *Live) otherRecords(ctx context.Context) map[string]Report {
	out := l.records(ctx)
	l.mu.Lock()
	delete(out, l.rep.ID)
	l.mu.Unlock()
	return out
}

// Start shares the settings with the other replicas through rep until
// Stop: it puts each save into effect, renews the record of the replica
// and, every rep.Every, looks for a save whose announcement it missed.
func (l *Live) Start(ctx context.Context, rep Replica) {
	l.mu.Lock()
	l.rep = rep
	l.report.Replica = rep.ID
	l.publish(ctx)
	l.mu.Unlock()
	if rep.PubSub == nil {
		return
	}
	sub := rep.PubSub.Subscribe(changedTopic)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.stopMu.Lock()
	l.cancel, l.done = cancel, done
	l.stopMu.Unlock()
	go func() {
		defer close(done)
		defer func() { _ = sub.Close() }()
		ticker := time.NewTicker(rep.every())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-sub.Channel():
				if !ok {
					return
				}
				l.syncLogged(ctx)
			case <-ticker.C:
				l.syncLogged(ctx)
				l.mu.Lock()
				l.publishRecord(ctx)
				l.mu.Unlock()
			}
		}
	}()
}

func (l *Live) publishRecord(ctx context.Context) {
	if b, err := json.Marshal(l.report); err == nil {
		l.writeRecord(ctx, b)
	}
}

func (l *Live) syncLogged(ctx context.Context) {
	if _, err := l.Sync(ctx); err != nil {
		slog.Error("Failed to take the saved Setup settings", "error", err)
	}
}

// Stop ends what Start runs and removes the record of the replica. A
// second Stop does nothing.
func (l *Live) Stop(ctx context.Context) {
	l.stopMu.Lock()
	if l.stopped {
		l.stopMu.Unlock()
		return
	}
	l.stopped = true
	cancel, done := l.cancel, l.done
	l.cancel, l.done = nil, nil
	l.stopMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rep.KV != nil && l.rep.ID != "" {
		if err := l.rep.KV.Delete(ctx, recordPrefix+l.rep.ID); err != nil {
			slog.Error("Failed to remove the record of the Setup settings of this replica", "error", err)
		}
	}
}
