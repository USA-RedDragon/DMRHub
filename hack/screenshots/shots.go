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
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	shotStep       = 45 * time.Second
	pageAge        = 2 * time.Second
	frozenWait     = 12 * time.Second
	afterStep      = 30 * time.Second
	requestStep    = 100 * time.Millisecond
	pingWait       = 15 * time.Second
	keepaliveEvery = 5 * time.Second
)

type hook func(ctx context.Context, r *runner) error

type runner struct {
	o       options
	l       *shotList
	c       *clock
	b       *browser
	base    string
	cursor  time.Time
	as      string
	apis    map[string]*api
	peers   map[uint32]*peer
	secrets *secrets
	stream  uint
	hooks   map[string]hook
	done    map[string]bool
	stop    []func()
	peersMu sync.Mutex
}

func newRunner(o options, l *shotList, c *clock, base string, start time.Time) *runner {
	return &runner{o: o, l: l, c: c, base: base, cursor: start, apis: map[string]*api{}, peers: map[uint32]*peer{}, secrets: newSecrets(), hooks: hooks(), done: map[string]bool{}}
}

func (r *runner) api(user string) *api {
	if a, ok := r.apis[user]; ok {
		return a
	}
	a := newAPI(r.base, r.secrets)
	a.before = r.beforeRequest
	r.apis[user] = a
	return a
}

func (r *runner) tick(d time.Duration) error {
	r.cursor = r.cursor.Add(d)
	if err := r.c.set(r.cursor); err != nil {
		return err
	}
	return r.pingAll()
}

func (r *runner) advance(d time.Duration) error {
	r.cursor = r.cursor.Add(d)
	r.c.freeze(r.cursor)
	return r.pingAll()
}

func (r *runner) beforeRequest() {
	if r.c.frozen() {
		r.cursor = r.cursor.Add(requestStep)
		r.c.freeze(r.cursor)
	}
}

func (r *runner) waitPings(ctx context.Context) error {
	deadline := time.Now().Add(pingWait)
	for id := range r.peers {
		for {
			var rp struct {
				LastPing time.Time `json:"last_ping_time"`
			}
			err := r.api("n0call").do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repeaters/%d", id), nil, &rp)
			if err == nil && !rp.LastPing.Before(r.cursor) {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("repeater %d: DMRHub did not save the last ping: %w", id, errTimeout)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}

func (r *runner) pingAll() error {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()
	ids := make([]uint32, 0, len(r.peers))
	for id := range r.peers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := r.peers[id].ping(); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) addPeer(id uint32, p *peer) {
	r.peersMu.Lock()
	r.peers[id] = p
	r.peersMu.Unlock()
}

func (r *runner) keepalive(ctx context.Context) {
	t := time.NewTicker(keepaliveEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if r.c.frozen() {
				_ = r.pingAll()
			}
		}
	}
}

func passwordOf(user string) string {
	if user == "n0call" {
		return adminPassword
	}
	return userPassword
}

func (r *runner) loginBrowser(ctx context.Context, user string) error {
	if user == "" {
		user = "guest"
	}
	if user == r.as {
		return nil
	}
	if _, err := r.b.call(ctx, "Network.clearBrowserCookies", nil); err != nil {
		return err
	}
	r.as = user
	if user == "guest" {
		return nil
	}
	if err := r.b.navigate(ctx, r.base+"/login"); err != nil {
		return err
	}
	if err := r.fill(ctx, "Username or callsign", user); err != nil {
		return err
	}
	if err := r.fill(ctx, "Password", passwordOf(user)); err != nil {
		return err
	}
	if err := r.b.click(ctx, "Log in"); err != nil {
		return err
	}
	return r.b.waitText(ctx, "Logout")
}

func (r *runner) take(ctx context.Context, s shot) error {
	slog.Info("Taking", "shot", s.Name)
	if err := r.advance(shotStep); err != nil {
		return err
	}
	for _, name := range s.Needs {
		if r.done[name] {
			continue
		}
		h, ok := r.hooks[name]
		if !ok {
			return fmt.Errorf("shot %s: unknown state %q", s.Name, name)
		}
		slog.Info("State", "name", name)
		if err := h(ctx, r); err != nil {
			return fmt.Errorf("shot %s: state %s: %w", s.Name, name, err)
		}
		r.done[name] = true
	}
	if s.Frozen {
		r.cursor = r.cursor.Add(shotStep)
		r.c.freeze(r.cursor)
		if err := r.pingAll(); err != nil {
			return err
		}
		time.Sleep(frozenWait)
	} else if err := r.tick(shotStep); err != nil {
		return err
	}
	if s.Phase == "main" && !s.Frozen {
		if err := r.waitPings(ctx); err != nil {
			return fmt.Errorf("shot %s: %w", s.Name, err)
		}
	}
	time.Sleep(s.Pause)
	r.b.at = r.cursor.Add(pageAge)
	if s.Phase == "main" {
		if err := r.loginBrowser(ctx, s.As); err != nil {
			return fmt.Errorf("shot %s: %w", s.Name, err)
		}
	}
	if s.Route != "" {
		route := r.base + s.Route
		if strings.HasPrefix(s.Route, "radio:") {
			route = radioURL + strings.TrimPrefix(s.Route, "radio:")
		}
		if err := r.b.navigate(ctx, route); err != nil {
			return fmt.Errorf("shot %s: %w", s.Name, err)
		}
	}
	if s.Wait != "" {
		if err := r.b.waitText(ctx, s.Wait); err != nil {
			return fmt.Errorf("shot %s: the page does not show its demo data: %w", s.Name, err)
		}
	}
	if len(s.Steps) > 0 {
		if err := r.b.settle(ctx); err != nil {
			return fmt.Errorf("shot %s: %w", s.Name, err)
		}
	}
	for _, st := range s.Steps {
		if err := r.step(ctx, st); err != nil {
			return fmt.Errorf("shot %s: step %q: %w", s.Name, st, err)
		}
	}
	if err := r.b.settle(ctx); err != nil {
		return fmt.Errorf("shot %s: %w", s.Name, err)
	}
	r.b.waitSecrets()
	if err := r.b.mask(ctx, r.secrets.all()); err != nil {
		return fmt.Errorf("shot %s: %w", s.Name, err)
	}
	if err := r.b.shot(ctx, filepath.Join(r.o.out, s.Name), s.Clip); err != nil {
		return fmt.Errorf("shot %s: %w", s.Name, err)
	}
	if err := r.advance(afterStep); err != nil {
		return err
	}
	for _, st := range s.After {
		if err := r.step(ctx, st); err != nil {
			return fmt.Errorf("shot %s: after step %q: %w", s.Name, st, err)
		}
	}
	return nil
}

func (r *runner) step(ctx context.Context, st string) error {
	verb, arg, _ := strings.Cut(st, " ")
	label, value, _ := strings.Cut(arg, " = ")
	switch verb {
	case "fill":
		return r.fill(ctx, label, value)
	case "pick":
		return r.pick(ctx, label, value)
	case "toggle":
		return r.toggle(ctx, arg)
	case "click":
		return r.b.click(ctx, arg)
	case "dclick":
		return r.dialogClick(ctx, arg)
	case "clicktest":
		return r.b.clickTest(ctx, arg)
	case "clickcss":
		return r.clickCSS(ctx, arg)
	case "wait":
		return r.b.waitText(ctx, arg)
	case "scroll":
		return r.b.scrollTo(ctx, arg)
	case "blur":
		return r.b.eval(ctx, "document.activeElement && document.activeElement.blur()", nil)
	case "file":
		return r.b.setFiles(ctx, label, filepath.Join(r.o.work, value))
	case "key":
		for _, t := range []string{"keyDown", "keyUp"} {
			if _, err := r.b.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": t, "key": arg, "code": arg}); err != nil {
				return err
			}
		}
		return nil
	case "sleep":
		d, err := time.ParseDuration(arg)
		if err != nil {
			return fmt.Errorf("bad sleep: %w", err)
		}
		time.Sleep(d)
		return nil
	case "goto":
		return r.b.navigate(ctx, r.base+arg)
	case "run":
		h, ok := r.hooks[arg]
		if !ok {
			return fmt.Errorf("unknown state %q", arg)
		}
		return h(ctx, r)
	}
	return fmt.Errorf("unknown step %q", verb)
}

const findControl = `((label) => {
	const norm = (s) => s.trim().replace(/\s+/g, ' ');
	const l = [...document.querySelectorAll('label')].find((e) => norm(e.textContent) === label) ||
		[...document.querySelectorAll('label')].find((e) => norm(e.textContent).startsWith(label));
	if (!l) return null;
	if (l.htmlFor) return document.getElementById(l.htmlFor);
	return l.querySelector('input, select, textarea, button') || l.parentElement.querySelector('input, select, textarea, button');
})`

func (r *runner) control(ctx context.Context, label string) (string, error) {
	q, err := json.Marshal(label)
	if err != nil {
		return "", fmt.Errorf("bad label: %w", err)
	}
	find := findControl + "(" + string(q) + ")"
	if strings.HasPrefix(label, "#") || strings.HasPrefix(label, "[") {
		find = "document.querySelector(" + string(q) + ")"
	}
	if err := r.b.waitFor(ctx, find); err != nil {
		return "", err
	}
	return find, nil
}

func (r *runner) fill(ctx context.Context, label, value string) error {
	find, err := r.control(ctx, label)
	if err != nil {
		return err
	}
	if err := r.b.eval(ctx, `(() => {
		const e = `+find+`;
		e.focus();
		const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(e), 'value').set;
		set.call(e, '');
		e.dispatchEvent(new Event('input', { bubbles: true }));
	})()`, nil); err != nil {
		return err
	}
	if value == "" {
		return nil
	}
	_, err = r.b.call(ctx, "Input.insertText", map[string]any{"text": value})
	return err
}

func (r *runner) pick(ctx context.Context, label, option string) error {
	find, err := r.control(ctx, label)
	if err != nil {
		return err
	}
	o, err := json.Marshal(strings.Split(option, " | "))
	if err != nil {
		return fmt.Errorf("bad option: %w", err)
	}
	have := "((e, want) => [...e.options].filter((x) => want.includes(x.textContent.trim()) || want.includes(x.value)).length === want.length)(" + find + ", " + string(o) + ")"
	if err := r.b.waitFor(ctx, have); err != nil {
		return fmt.Errorf("%s has no option %q: %w", label, option, err)
	}
	var ok bool
	err = r.b.eval(ctx, `(() => {
		const e = `+find+`;
		const want = `+string(o)+`;
		const opts = [...e.options];
		const hit = opts.filter((x) => want.includes(x.textContent.trim()) || want.includes(x.value));
		if (hit.length !== want.length) return false;
		if (e.multiple) { for (const x of opts) x.selected = hit.includes(x); } else { e.value = hit[0].value; }
		e.dispatchEvent(new Event('input', { bubbles: true }));
		e.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	})()`, &ok)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s has no option %q", label, option)
	}
	return nil
}

func (r *runner) toggle(ctx context.Context, label string) error {
	find, err := r.control(ctx, label)
	if err != nil {
		return err
	}
	return r.b.eval(ctx, "("+find+").click()", nil)
}

func (r *runner) dialogClick(ctx context.Context, text string) error {
	t, err := json.Marshal(text)
	if err != nil {
		return fmt.Errorf("bad text: %w", err)
	}
	find := "[...document.querySelectorAll('[role=dialog] button, [role=alertdialog] button')].find((e) => e.innerText.includes(" + string(t) + "))"
	if err := r.b.waitFor(ctx, find); err != nil {
		return err
	}
	return r.b.eval(ctx, "("+find+").click()", nil)
}

func (r *runner) clickCSS(ctx context.Context, css string) error {
	q, err := json.Marshal(css)
	if err != nil {
		return fmt.Errorf("bad selector: %w", err)
	}
	if err := r.b.waitFor(ctx, "document.querySelector("+string(q)+")"); err != nil {
		return err
	}
	return r.b.eval(ctx, "document.querySelector("+string(q)+").click()", nil)
}

func runSetup(ctx context.Context, o options, l *shotList, c *clock) error {
	start := l.Start.Add(-time.Hour)
	if err := c.set(start); err != nil {
		return err
	}
	hub, err := startHub(ctx, o, l, "setup.log")
	if err != nil {
		return err
	}
	defer hub.stop()
	r := newRunner(o, l, c, wizardURL, start)
	if err := r.api("wizard").waitUp(ctx); err != nil {
		return err
	}
	status, err := l.status()
	if err != nil {
		return err
	}
	if r.b, err = startBrowser(ctx, o.chromium, o.work, l.TimeZone, status, r.secrets); err != nil {
		return err
	}
	defer r.b.close()
	for _, s := range l.phase("setup", o.only) {
		if err := r.take(ctx, s); err != nil {
			return err
		}
	}
	if err := newAPI(wizardURL, nil).waitFor(ctx, "/api/v1/network/features"); err != nil {
		return fmt.Errorf("DMRHub did not start after the setup: %w", err)
	}
	return nil
}

func runMain(ctx context.Context, o options, l *shotList, c *clock) error {
	if err := c.set(l.Start); err != nil {
		return err
	}
	mail, err := startMailSink(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = mail.Close() }()
	radio, err := startRadio(ctx, o.radio)
	if err != nil {
		return err
	}
	defer func() { _ = radio.Close() }()
	hub, err := startHub(ctx, o, l, "main.log")
	if err != nil {
		return err
	}
	defer hub.stop()
	r := newRunner(o, l, c, fmt.Sprintf("http://127.0.0.1:%d", httpPort), l.Start)
	defer r.closePeers()
	kctx, stopKeepalive := context.WithCancel(ctx)
	defer stopKeepalive()
	go r.keepalive(kctx)
	if err := r.api("n0call").waitUp(ctx); err != nil {
		return err
	}
	if err := r.api("n0call").login(ctx, "n0call", adminPassword); err != nil {
		return err
	}
	status, err := l.status()
	if err != nil {
		return err
	}
	if r.b, err = startBrowser(ctx, o.chromium, o.work, l.TimeZone, status, r.secrets); err != nil {
		return err
	}
	defer r.b.close()
	for _, s := range l.phase("main", o.only) {
		if err := r.take(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) closePeers() {
	for _, f := range r.stop {
		f()
	}
	for _, p := range r.peers {
		p.close()
	}
}

type secrets struct {
	mu     sync.Mutex
	values map[string]string
	counts map[string]int
	last   map[string]string
}

func newSecrets() *secrets {
	return &secrets{values: map[string]string{}, counts: map[string]int{}, last: map[string]string{}}
}

var secretKey = regexp.MustCompile(`^(password|temporary_password|token|auth_key|fingerprint|secret|access_token|refresh_token)$`)

func (s *secrets) scan(v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if str, ok := t[k].(string); ok && secretKey.MatchString(k) && len(str) >= 8 {
				s.add(k, str)
				continue
			}
			s.scan(t[k])
		}
	case []any:
		for _, x := range t {
			s.scan(x)
		}
	}
}

func (s *secrets) add(kind, value string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last[kind] = value
	if _, ok := s.values[value]; ok {
		return
	}
	s.counts[kind]++
	n := s.counts[kind]
	switch kind {
	case "fingerprint":
		s.values[value] = strings.Repeat(fmt.Sprintf("%02x", n), 32)
	case "auth_key":
		s.values[value] = strings.Repeat(fmt.Sprintf("%02x", n), 20)
	case "token", "access_token", "refresh_token":
		s.values[value] = fmt.Sprintf("example-token-%d", n)
	default:
		s.values[value] = fmt.Sprintf("Example-Password-%d", n)
	}
}

func (s *secrets) all() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.values)
}

func (s *secrets) latest(kind string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last[kind]
}
