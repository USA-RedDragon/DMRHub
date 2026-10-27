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
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// shot is one screenshot: the page, a text to wait for, and what to do
// before the capture.
type shot struct {
	file   string
	path   string
	wait   string
	before func(ctx context.Context, b *browser) error
}

func scrollTo(title string) func(ctx context.Context, b *browser) error {
	return func(ctx context.Context, b *browser) error {
		return b.scrollTo(ctx, title)
	}
}

// mapTime is how long the map gets to load its tiles, as it shows no sign
// in the page when it is done.
const mapTime = 8 * time.Second

func showRadio(test string) func(ctx context.Context, b *browser) error {
	return func(ctx context.Context, b *browser) error {
		time.Sleep(mapTime)
		if err := b.clickTest(ctx, test); err != nil {
			return err
		}
		time.Sleep(2 * time.Second)
		return nil
	}
}

func userShots() []shot {
	return []shot{
		{file: "home.png", path: "/", wait: "Welcome to DMRHub"},
		{file: "lastheard.png", path: "/lastheard", wait: "N0FAKE"},
		{file: "repeaters.png", path: "/repeaters", wait: "300000201"},
		{file: "repeater-details.png", path: "/repeaters/300000201", wait: "300000201"},
		{file: "talkgroups.png", path: "/talkgroups", wait: "Demo Talkgroup"},
		{file: "talkgroup-details.png", path: "/talkgroups/3000", wait: "Demo Talkgroup"},
		{file: "talkgroup-management.png", path: "/talkgroups/owned", wait: "Demo Talkgroup"},
		{file: "nets.png", path: "/nets", wait: "Weekly Demo Net"},
		{file: "net-details.png", path: "/nets/1", wait: "N0TEST"},
		{file: "net-schedule.png", path: "/nets/scheduled/new", wait: "Talkgroup"},
		{file: "settings.png", path: "/settings", wait: "SMS Storage", before: scrollTo("SMS Storage")},
		{file: "settings-apps.png", path: "/settings", wait: "localhost:8080", before: scrollTo("Apps")},
		{file: "user-details.png", path: "/users/3000002", wait: "N0DEMO"},
	}
}

func laterShots() []shot {
	return []shot{
		{file: "messages.png", path: "/messages", wait: "N0TEST"},
		{file: "positions.png", path: "/positions", wait: "3000012", before: showRadio("radio-3000002")},
		{file: "radio-positions.png", path: "/positions/3000002", wait: "3000002", before: func(context.Context, *browser) error { time.Sleep(mapTime); return nil }},
	}
}

func adminShots() []shot {
	return []shot{
		{file: "admin-users.png", path: "/admin/users", wait: "N0FAKE"},
		{file: "admin-users-approval.png", path: "/admin/users/approval", wait: "N0NONE"},
		{file: "admin-repeaters.png", path: "/admin/repeaters", wait: "300000401"},
		{file: "admin-talkgroups.png", path: "/admin/talkgroups", wait: "Demo Talkgroup"},
		{file: "admin-talkgroups-new.png", path: "/admin/talkgroups/new", wait: "Talkgroup"},
		{file: "admin-peers.png", path: "/admin/peers", wait: "300090"},
		{file: "admin-peers-new.png", path: "/admin/peers/new", wait: "Peer"},
		{file: "admin-messages.png", path: "/admin/messages", wait: "N0TEST"},
		{file: "admin-plugins.png", path: "/admin/plugins", wait: "Parrot"},
		{file: "admin-nets.png", path: "/admin/nets", wait: "Demo Talkgroup"},
		{file: "admin-setup.png", path: "/admin/setup", wait: "General Settings"},
	}
}

func (s *session) take(ctx context.Context, list []shot) error {
	for _, sh := range list {
		if err := s.b.navigate(ctx, s.base+sh.path); err != nil {
			return err
		}
		if err := s.b.waitText(ctx, sh.wait); err != nil {
			s.missing = append(s.missing, sh.file)
			slog.Warn("The page does not show what it should", "file", sh.file, "error", err)
		}
		if sh.before != nil {
			if err := sh.before(ctx, s.b); err != nil {
				return fmt.Errorf("%s: %w", sh.file, err)
			}
		}
		if err := s.b.shot(ctx, filepath.Join(s.out, sh.file)); err != nil {
			return err
		}
	}
	return nil
}

// session is the main phase: the hub, its users and the browser.
type session struct {
	base     string
	out      string
	password string
	admin    *api
	users    map[string]*api
	peers    map[uint32]*peer
	b        *browser
	missing  []string
	stream   uint
}

type demoUser struct {
	id       uint
	callsign string
	approve  bool
}

func demoUsers() []demoUser {
	return []demoUser{
		{3000002, "N0DEMO", true},
		{3000003, "N0TEST", true},
		{3000004, "N0FAKE", true},
		{3000005, "N0NONE", false},
	}
}

type demoRepeater struct {
	id    uint32
	owner string
	typ   string
	site  site
	ts1   []uint
	ts2   []uint
}

func demoRepeaters() []demoRepeater {
	return []demoRepeater{
		{300001, "N0CALL", "mmdvm", site{"N0CALL", 444125000, 449125000, 32.7831, -96.8067, "Dallas, TX", "Demo repeater", 3}, []uint{1, 8}, []uint{2, 3000}},
		{300000201, "N0DEMO", "mmdvm", site{"N0DEMO", 433450000, 433450000, 32.7767, -96.7970, "Dallas, TX", "Demo hotspot", 4}, nil, []uint{3000}},
		{300000301, "N0TEST", "mmdvm", site{"N0TEST", 438800000, 438800000, 32.9483, -96.7299, "Richardson, TX", "Demo hotspot", 4}, nil, []uint{3000, 2}},
		{300000401, "N0FAKE", "mmdvm", site{"N0FAKE", 434000000, 434000000, 33.0198, -96.6989, "Plano, TX", "Demo hotspot", 4}, nil, []uint{8}},
		{300002, "N0CALL", "ipsc", site{}, nil, nil},
	}
}

func usernameOf(callsign string) string {
	return strings.ToLower(callsign)
}

func runMain(ctx context.Context, o options) error {
	c, err := readCreds(o)
	if err != nil {
		return err
	}
	hub, err := startHub(ctx, o, "main.log")
	if err != nil {
		return err
	}
	defer hub.stop()
	s := &session{base: fmt.Sprintf("http://127.0.0.1:%d", httpPort), out: o.out, users: map[string]*api{}, peers: map[uint32]*peer{}}
	s.admin = newAPI(s.base)
	if err := s.admin.waitUp(ctx); err != nil {
		return err
	}
	if err := s.admin.login(ctx, c.Username, c.Password); err != nil {
		return err
	}
	s.users["N0CALL"] = s.admin
	if s.password, err = randomString(12); err != nil {
		return err
	}
	defer s.closePeers()
	if err := s.seed(ctx); err != nil {
		return err
	}
	if err := s.play(voiceCalls()); err != nil {
		return err
	}
	positions := make(chan error, 1)
	go func() { positions <- s.reportPositions() }()
	if s.b, err = startBrowser(ctx, o.chromium, o.work); err != nil {
		return err
	}
	defer s.b.close()
	if err := s.userPages(ctx); err != nil {
		return err
	}
	if err := <-positions; err != nil {
		return err
	}
	if err := s.play(textMessages()); err != nil {
		return err
	}
	if err := s.take(ctx, laterShots()); err != nil {
		return err
	}
	if err := s.loginBrowser(ctx, c.Username, c.Password, ""); err != nil {
		return err
	}
	if err := s.take(ctx, adminShots()); err != nil {
		return err
	}
	if len(s.missing) > 0 {
		return fmt.Errorf("pages without their demo data: %s", strings.Join(s.missing, ", "))
	}
	return nil
}

func (s *session) seed(ctx context.Context) error {
	steps := []func(context.Context) error{s.seedUsers, s.seedTalkgroups, s.seedRepeaters, s.seedSettings, s.seedNets, s.seedPeer}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) seedUsers(ctx context.Context) error {
	for _, u := range demoUsers() {
		a := newAPI(s.base)
		if err := a.do(ctx, http.MethodPost, "/api/v1/users", map[string]any{"id": u.id, "callsign": u.callsign, "username": usernameOf(u.callsign), "password": s.password}, nil); err != nil {
			return err
		}
		if !u.approve {
			continue
		}
		if err := s.admin.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/approve/%d", u.id), nil, nil); err != nil {
			return err
		}
		if err := a.login(ctx, usernameOf(u.callsign), s.password); err != nil {
			return err
		}
		s.users[u.callsign] = a
	}
	return nil
}

func (s *session) seedTalkgroups(ctx context.Context) error {
	tgs := []struct {
		id         uint
		name, desc string
	}{
		{1, "Worldwide", "Calls to every repeater and hotspot on the network"},
		{2, "Local", "Calls that stay on the local area"},
		{8, "Regional", "Calls across the region"},
		{91, "Chat", "Open talkgroup for ragchew"},
		{3000, "Demo Talkgroup", "Talkgroup of the weekly demo net"},
	}
	for _, tg := range tgs {
		if err := s.admin.do(ctx, http.MethodPost, "/api/v1/talkgroups", map[string]any{"id": tg.id, "name": tg.name, "description": tg.desc}, nil); err != nil {
			return err
		}
	}
	if err := s.admin.do(ctx, http.MethodPost, "/api/v1/talkgroups/3000/admins", map[string]any{"user_ids": []uint{3000002}}, nil); err != nil {
		return err
	}
	return s.users["N0DEMO"].do(ctx, http.MethodPost, "/api/v1/talkgroups/3000/ncos", map[string]any{"user_ids": []uint{3000003}}, nil)
}

func talkgroupList(ids []uint) []map[string]uint {
	out := make([]map[string]uint, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]uint{"id": id})
	}
	return out
}

func (s *session) seedRepeaters(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", mmdvmPort)
	for _, r := range demoRepeaters() {
		a := s.users[r.owner]
		var res struct {
			Password string `json:"password"`
		}
		if err := a.do(ctx, http.MethodPost, "/api/v1/repeaters", map[string]any{"id": r.id, "type": r.typ}, &res); err != nil {
			return err
		}
		if r.typ != "mmdvm" {
			continue
		}
		body := map[string]any{"ts1_static_talkgroups": talkgroupList(r.ts1), "ts2_static_talkgroups": talkgroupList(r.ts2)}
		if err := a.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repeaters/%d/talkgroups", r.id), body, nil); err != nil {
			return err
		}
		p, err := dialPeer(addr, r.id, res.Password, r.site)
		if err != nil {
			return err
		}
		s.peers[r.id] = p
	}
	return nil
}

func (s *session) seedSettings(ctx context.Context) error {
	on := map[string]bool{"enabled": true}
	for _, call := range []string{"N0CALL", "N0DEMO", "N0TEST"} {
		if err := s.users[call].do(ctx, http.MethodPut, "/api/v1/users/me/settings/sms-storage", on, nil); err != nil {
			return err
		}
	}
	if err := s.admin.do(ctx, http.MethodPost, "/api/v1/users/3000002/radio-ids", map[string]uint{"id": 3000012}, nil); err != nil {
		return err
	}
	tracked := map[string][]uint{"N0DEMO": {3000002, 3000012}, "N0TEST": {3000003}}
	for call, radios := range tracked {
		a := s.users[call]
		if err := a.do(ctx, http.MethodPut, "/api/v1/users/me/settings/share-positions", on, nil); err != nil {
			return err
		}
		for _, r := range radios {
			body := map[string]any{"tracking": true, "interval_minutes": 5, "retention_hours": 24}
			if err := a.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/positions/%d/settings", r), body, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *session) seedNets(ctx context.Context) error {
	demo := s.users["N0DEMO"]
	dur := 60
	if err := demo.do(ctx, http.MethodPost, "/api/v1/nets/scheduled", map[string]any{
		"talkgroup_id": 3000, "name": "Weekly Demo Net", "description": "Check in with your callsign and location",
		"day_of_week": 2, "time_of_day": "19:00", "timezone": "America/Chicago", "duration_minutes": dur,
	}, nil); err != nil {
		return err
	}
	return demo.do(ctx, http.MethodPost, "/api/v1/nets/start", map[string]any{"talkgroup_id": 3000, "description": "Weekly Demo Net", "duration_minutes": dur}, nil)
}

func (s *session) seedPeer(ctx context.Context) error {
	return s.admin.do(ctx, http.MethodPost, "/api/v1/peers", map[string]any{
		"id": 300090, "owner": 3000001, "ip": "127.0.0.1", "port": 62999, "ingress": true, "egress": true,
	}, nil)
}

func (s *session) closePeers() {
	for _, p := range s.peers {
		p.close()
	}
}

type call struct {
	repeater uint32
	src, dst uint
	group    bool
	slot2    bool
	frames   int
	text     string
}

func voiceCalls() []call {
	return []call{
		{repeater: 300001, src: 3000001, dst: 1, group: true, frames: 60},
		{repeater: 300000201, src: 3000002, dst: 3000, group: true, slot2: true, frames: 45},
		{repeater: 300000301, src: 3000003, dst: 3000, group: true, slot2: true, frames: 38},
		{repeater: 300000401, src: 3000004, dst: 3000, group: true, slot2: true, frames: 30},
		{repeater: 300000401, src: 3000004, dst: 8, group: true, slot2: true, frames: 25},
		{repeater: 300000301, src: 3000003, dst: 9990, slot2: true, frames: 40},
		{repeater: 300001, src: 3000001, dst: 2, group: true, slot2: true, frames: 35},
		{repeater: 300000201, src: 3000002, dst: 3000003, slot2: true, frames: 30},
		{repeater: 300000201, src: 3000002, dst: 3000, group: true, slot2: true, frames: 50},
		{repeater: 300000401, src: 3000004, dst: 1, group: true, slot2: true, frames: 28},
		{repeater: 300000201, src: 3000002, dst: 4000, group: true, slot2: true, frames: 6},
	}
}

func textMessages() []call {
	return []call{
		{repeater: 300000301, src: 3000003, dst: 3000002, slot2: true, text: "Hi N0DEMO, see you on the net tonight"},
		{repeater: 300000201, src: 3000002, dst: 3000003, slot2: true, text: "Thanks N0TEST, I will be there"},
		{repeater: 300000401, src: 3000004, dst: 3000, group: true, slot2: true, text: "Net control: N0DEMO tonight"},
		{repeater: 300000301, src: 3000003, dst: 3000001, slot2: true, text: "N0CALL, the repeater sounds good"},
		{repeater: 300000201, src: 3000002, dst: 3000003, slot2: true, text: "Can you bring the spare antenna?"},
		{repeater: 300000401, src: 3000004, dst: 3000002, slot2: true, text: "N0DEMO, QSL on the demo net"},
	}
}

func (s *session) play(calls []call) error {
	for _, c := range calls {
		var bursts []burst
		var err error
		if c.text != "" {
			bursts, err = textMessage(c.group, c.dst, c.src, c.text)
		} else {
			bursts, err = voiceCall(c.group, c.dst, c.src, c.frames)
		}
		if err != nil {
			return err
		}
		s.stream++
		if err := s.peers[c.repeater].play(transmission{src: c.src, dst: c.dst, group: c.group, slot2: c.slot2, bursts: bursts}, s.stream); err != nil {
			return err
		}
		time.Sleep(1500 * time.Millisecond)
	}
	time.Sleep(3 * time.Second)
	return nil
}

// track is the positions that a radio reports, one each 31 seconds, as
// the hub keeps at most one each 30 seconds.
type track struct {
	repeater uint32
	radio    uint
	points   [][2]float64
}

func demoTracks() []track {
	return []track{
		{300000201, 3000002, [][2]float64{{32.7767, -96.7970}, {32.7810, -96.7905}, {32.7862, -96.7851}, {32.7921, -96.7812}, {32.7985, -96.7790}}},
		{300000201, 3000012, [][2]float64{{32.8140, -96.8720}, {32.8102, -96.8655}, {32.8071, -96.8590}, {32.8049, -96.8521}, {32.8030, -96.8460}}},
		{300000301, 3000003, [][2]float64{{32.9483, -96.7299}, {32.9531, -96.7365}, {32.9580, -96.7428}, {32.9632, -96.7487}, {32.9690, -96.7544}}},
	}
}

func (s *session) reportPositions() error {
	stream := uint(50000)
	tracks := demoTracks()
	for i := range len(tracks[0].points) {
		if i > 0 {
			time.Sleep(31 * time.Second)
		}
		for _, t := range tracks {
			b, err := locationReport(t.radio, 5050, t.points[i][0], t.points[i][1], uint32(t.radio))
			if err != nil {
				return err
			}
			stream++
			if err := s.peers[t.repeater].play(transmission{src: t.radio, dst: 5050, slot2: true, bursts: b}, stream); err != nil {
				return err
			}
		}
	}
	time.Sleep(2 * time.Second)
	return nil
}

func (s *session) loginBrowser(ctx context.Context, username, password, loginShot string) error {
	if _, err := s.b.call(ctx, "Network.clearBrowserCookies", nil); err != nil {
		return err
	}
	if err := s.b.navigate(ctx, s.base+"/login"); err != nil {
		return err
	}
	if err := s.b.typeInto(ctx, "#username", username); err != nil {
		return err
	}
	if err := s.b.typeInto(ctx, "#password", password); err != nil {
		return err
	}
	if loginShot != "" {
		if err := s.b.eval(ctx, "document.activeElement.blur()", nil); err != nil {
			return err
		}
		if err := s.b.shot(ctx, filepath.Join(s.out, loginShot)); err != nil {
			return err
		}
	}
	if err := s.b.click(ctx, "Log in"); err != nil {
		return err
	}
	return s.b.waitText(ctx, "Logout")
}

func (s *session) userPages(ctx context.Context) error {
	if err := s.b.navigate(ctx, s.base+"/register"); err != nil {
		return err
	}
	if err := s.b.waitText(ctx, "Register"); err != nil {
		return err
	}
	if err := s.b.shot(ctx, filepath.Join(s.out, "register.png")); err != nil {
		return err
	}
	if err := s.loginBrowser(ctx, "n0demo", s.password, "login.png"); err != nil {
		return err
	}
	if err := s.addRepeater(ctx); err != nil {
		return err
	}
	if err := s.allowApp(ctx); err != nil {
		return err
	}
	return s.take(ctx, userShots())
}

// addRepeater adds a hotspot through the New Repeater page, as a user does.
func (s *session) addRepeater(ctx context.Context) error {
	if err := s.b.navigate(ctx, s.base+"/repeaters/new"); err != nil {
		return err
	}
	if err := s.b.typeInto(ctx, "#radioID", "300000202"); err != nil {
		return err
	}
	if err := s.b.eval(ctx, "document.activeElement.blur()", nil); err != nil {
		return err
	}
	if err := s.b.shot(ctx, filepath.Join(s.out, "repeaters-easy.png")); err != nil {
		return err
	}
	if err := s.b.click(ctx, "Save"); err != nil {
		return err
	}
	if err := s.b.waitText(ctx, "300000202"); err != nil {
		return err
	}
	if len(s.b.takeDialogs()) == 0 {
		return errors.New("the New Repeater page showed no configuration")
	}
	return nil
}

// allowApp allows a demo app on the consent page, so that Settings lists
// it.
func (s *session) allowApp(ctx context.Context) error {
	const app = "http://localhost:8080"
	const redirect = app + "/callback"
	verifier, err := randomString(32)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {app},
		"redirect_uri":          {redirect},
		"scope":                 {"talkgroups:read lastheard:read messages:read positions:read"},
		"state":                 {"demo"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	if err := s.b.navigate(ctx, s.base+"/oauth/authorize?"+q.Encode()); err != nil {
		return err
	}
	if err := s.b.waitText(ctx, "localhost:8080"); err != nil {
		return err
	}
	if err := s.b.shot(ctx, filepath.Join(s.out, "oauth-consent.png")); err != nil {
		return err
	}
	if err := s.b.click(ctx, "Allow"); err != nil {
		return err
	}
	if err := s.b.waitFor(ctx, "!location.href.startsWith('"+s.base+"')"); err != nil {
		return err
	}
	back, ok := s.b.requestedURL(redirect)
	if !ok {
		return errors.New("the consent page did not send the browser back to the app")
	}
	u, err := url.Parse(back)
	if err != nil {
		return fmt.Errorf("bad redirect: %w", err)
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {u.Query().Get("code")},
		"redirect_uri":  {redirect},
		"client_id":     {app},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/api/v1/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("bad token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request: %s", resp.Status)
	}
	return nil
}
