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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	appOrigin    = "http://localhost:8080"
	appRedirect  = appOrigin + "/callback"
	appVerifier  = "demo-code-verifier-0123456789-abcdefghijklmnopqrstuvwxyz"
	callSlot     = 10 * time.Second
	positionStep = 31 * time.Second
)

func hooks() map[string]hook {
	return map[string]hook{
		"configure":     configure,
		"seed":          seed,
		"hotspot-login": hotspotLogin,
		"call-local":    playCalls(localCall()),
		"call-parrot":   callParrot,
		"traffic":       playCalls(trafficCalls()),
		"net":           startNet,
		"net-checkins":  playCalls(netCalls()),
		"texts":         playCalls(textMessages()),
		"positions":     reportPositions,
		"allow-app":     finishApp,
		"peer":          seedPeer,
		"ended-net":     endNet,
		"rules":         repeaterRules,
		"email":         setEmail,
		"dmrgateway":    startDMRGateway,
	}
}

func configure(ctx context.Context, r *runner) error {
	wizard := r.api("wizard")
	var cfg map[string]any
	if err := wizard.do(ctx, http.MethodGet, "/api/v1/config", nil, &cfg); err != nil {
		return err
	}
	delete(cfg, "secrets")
	delete(cfg, "secret")
	delete(cfg, "password-salt")
	set := func(path string, v any) {
		m := cfg
		parts := strings.Split(path, ".")
		for _, p := range parts[:len(parts)-1] {
			next, ok := m[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				m[p] = next
			}
			m = next
		}
		m[parts[len(parts)-1]] = v
	}
	set("network-name", "DMRHub Demo")
	set("http.canonical-host", fmt.Sprintf("http://127.0.0.1:%d", httpPort))
	set("dmr.ipsc.network-id", 310999)
	set("dmr.disable-radio-id-validation", true)
	set("dmr.radio-id-url", "http://127.0.0.1:9/users.json")
	set("dmr.repeater-id-url", "http://127.0.0.1:9/rptrs.json")
	return wizard.do(ctx, http.MethodPut, "/api/v1/config", cfg, nil)
}

type demoUser struct {
	id       uint
	callsign string
	approve  bool
}

func demoUsers() []demoUser {
	return []demoUser{
		{3000002, "N1CALL", true},
		{3000003, "N2CALL", true},
		{3000004, "N3CALL", true},
		{3000005, "N4CALL", false},
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
		{310001, "n0call", "mmdvm", site{"N0CALL", 444125000, 449125000, 32.7831, -96.8067, "Dallas, TX", "Demo repeater", 3}, []uint{1, 8}, []uint{3100}},
		{300000201, "n1call", "mmdvm", site{"N1CALL", 433450000, 433450000, 32.7767, -96.7970, "Dallas, TX", "Demo hotspot", 4}, nil, []uint{3100}},
		{300000301, "n2call", "mmdvm", site{"N2CALL", 438800000, 438800000, 32.9483, -96.7299, "Richardson, TX", "Demo hotspot", 4}, nil, []uint{3100, 8}},
		{300000401, "n3call", "mmdvm", site{"N3CALL", 434000000, 434000000, 33.0198, -96.6989, "Plano, TX", "Demo hotspot", 4}, nil, []uint{8}},
		{310002, "n0call", "ipsc", site{}, nil, nil},
	}
}

func usernameOf(callsign string) string {
	return strings.ToLower(callsign)
}

func talkgroupList(ids []uint) []map[string]uint {
	out := make([]map[string]uint, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]uint{"id": id})
	}
	return out
}

func seed(ctx context.Context, r *runner) error {
	admin := r.api("n0call")
	for _, u := range demoUsers() {
		name := usernameOf(u.callsign)
		a := r.api(name)
		if err := a.do(ctx, http.MethodPost, "/api/v1/users", map[string]any{"id": u.id, "callsign": u.callsign, "username": name, "password": userPassword}, nil); err != nil {
			return err
		}
		if !u.approve {
			continue
		}
		if err := admin.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/approve/%d", u.id), nil, nil); err != nil {
			return err
		}
		if err := a.login(ctx, name, userPassword); err != nil {
			return err
		}
	}
	tgs := []struct {
		id         uint
		name, desc string
	}{
		{8, "Regional", "Calls across the region"},
		{3100, "Demo Net", "Talkgroup of the weekly demo net"},
		{31665, "Bridge", "Talkgroup that the OpenBridge peer carries"},
	}
	for _, tg := range tgs {
		if err := admin.do(ctx, http.MethodPost, "/api/v1/talkgroups", map[string]any{"id": tg.id, "name": tg.name, "description": tg.desc}, nil); err != nil {
			return err
		}
	}
	if err := admin.do(ctx, http.MethodPost, "/api/v1/talkgroups/3100/admins", map[string]any{"user_ids": []uint{3000002}}, nil); err != nil {
		return err
	}
	if err := r.api("n1call").do(ctx, http.MethodPost, "/api/v1/talkgroups/3100/ncos", map[string]any{"user_ids": []uint{3000003}}, nil); err != nil {
		return err
	}
	if err := admin.do(ctx, http.MethodPost, "/api/v1/repeaters/300000101/talkgroups", map[string]any{"ts1_static_talkgroups": talkgroupList([]uint{91}), "ts2_static_talkgroups": talkgroupList([]uint{3100})}, nil); err != nil {
		return err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", mmdvmPort)
	for _, rp := range demoRepeaters() {
		a := r.api(rp.owner)
		var res struct {
			Password string `json:"password"`
		}
		if err := a.do(ctx, http.MethodPost, "/api/v1/repeaters", map[string]any{"id": rp.id, "type": rp.typ}, &res); err != nil {
			return err
		}
		if rp.typ != "mmdvm" {
			continue
		}
		body := map[string]any{"ts1_static_talkgroups": talkgroupList(rp.ts1), "ts2_static_talkgroups": talkgroupList(rp.ts2)}
		if err := a.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repeaters/%d/talkgroups", rp.id), body, nil); err != nil {
			return err
		}
		p, err := dialPeer(addr, rp.id, res.Password, rp.site)
		if err != nil {
			return err
		}
		r.addPeer(rp.id, p)
	}
	on := map[string]bool{"enabled": true}
	for _, u := range []string{"n0call", "n1call", "n2call"} {
		if err := r.api(u).do(ctx, http.MethodPut, "/api/v1/users/me/settings/sms-storage", on, nil); err != nil {
			return err
		}
	}
	if err := admin.do(ctx, http.MethodPost, "/api/v1/users/3000001/radio-ids", map[string]uint{"id": 3000011}, nil); err != nil {
		return err
	}
	tracked := map[string][]uint{"n0call": {3000001}, "n1call": {3000002}}
	for _, u := range []string{"n0call", "n1call"} {
		a := r.api(u)
		if err := a.do(ctx, http.MethodPut, "/api/v1/users/me/settings/share-positions", on, nil); err != nil {
			return err
		}
		for _, id := range tracked[u] {
			body := map[string]any{"tracking": true, "interval_minutes": 5, "retention_hours": 24}
			if err := a.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/positions/%d/settings", id), body, nil); err != nil {
				return err
			}
		}
	}
	return r.api("n1call").do(ctx, http.MethodPost, "/api/v1/nets/scheduled", map[string]any{
		"talkgroup_id": 3100, "name": "Weekly Demo Net", "description": "Check in with your callsign and location",
		"day_of_week": 4, "time_of_day": "19:00", "timezone": r.l.TimeZone, "duration_minutes": 60,
	}, nil)
}

func hotspotLogin(_ context.Context, r *runner) error {
	pw := r.secrets.latest("password")
	if pw == "" {
		return errors.New("the connect page showed no password")
	}
	p, err := dialPeer(fmt.Sprintf("127.0.0.1:%d", mmdvmPort), 300000101, pw, site{"N0CALL", 438800000, 438800000, 32.7767, -96.7970, "Dallas, TX", "Hotspot of N0CALL", 4})
	if err != nil {
		return err
	}
	r.addPeer(300000101, p)
	return nil
}

func seedPeer(ctx context.Context, r *runner) error {
	admin := r.api("n0call")
	if err := admin.do(ctx, http.MethodPatch, "/api/v1/peers/3000900", map[string]any{"egress_private": true}, nil); err != nil {
		return err
	}
	rules := []map[string]any{
		{"direction": true, "match": "destination", "subject_id_min": 91, "subject_id_max": 91},
		{"direction": true, "match": "destination", "subject_id_min": 3100, "subject_id_max": 3100},
		{"direction": false, "match": "destination", "subject_id_min": 3100, "subject_id_max": 3100},
		{"direction": false, "match": "destination", "subject_id_min": 31665, "subject_id_max": 31665},
		{"direction": false, "match": "source", "subject_id_min": 3000001, "subject_id_max": 3000099},
	}
	for _, rule := range rules {
		if err := admin.do(ctx, http.MethodPost, "/api/v1/peers/3000900/rules", rule, nil); err != nil {
			return err
		}
	}
	return nil
}

func startNet(ctx context.Context, r *runner) error {
	return r.api("n1call").do(ctx, http.MethodPost, "/api/v1/nets/start", map[string]any{"talkgroup_id": 3100, "description": "Weekly Demo Net", "duration_minutes": 60}, nil)
}

func endNet(ctx context.Context, r *runner) error {
	var nets []struct {
		ID     uint `json:"id"`
		Active bool `json:"active"`
	}
	var page struct {
		Nets []struct {
			ID     uint `json:"id"`
			Active bool `json:"active"`
		} `json:"nets"`
	}
	if err := r.api("n1call").do(ctx, http.MethodGet, "/api/v1/nets", nil, &page); err != nil {
		return err
	}
	nets = page.Nets
	for _, n := range nets {
		if n.Active {
			return r.api("n1call").do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/nets/%d/stop", n.ID), nil, nil)
		}
	}
	return errors.New("no active net")
}

type call struct {
	repeater uint32
	src, dst uint
	group    bool
	slot2    bool
	frames   int
	text     string
}

func localCall() []call {
	return []call{{repeater: 300000101, src: 3000001, dst: 1, group: true, slot2: true, frames: 40}}
}

func trafficCalls() []call {
	return []call{
		{repeater: 310001, src: 3000001, dst: 8, group: true, frames: 55},
		{repeater: 300000201, src: 3000002, dst: 3100, group: true, slot2: true, frames: 45},
		{repeater: 300000301, src: 3000003, dst: 8, group: true, slot2: true, frames: 30},
		{repeater: 300000401, src: 3000004, dst: 8, group: true, slot2: true, frames: 25},
		{repeater: 300000201, src: 3000002, dst: 3000003, slot2: true, frames: 30},
	}
}

func netCalls() []call {
	return []call{
		{repeater: 300000201, src: 3000002, dst: 3100, group: true, slot2: true, frames: 50},
		{repeater: 300000301, src: 3000003, dst: 3100, group: true, slot2: true, frames: 35},
		{repeater: 310001, src: 3000001, dst: 3100, group: true, slot2: true, frames: 30},
	}
}

func textMessages() []call {
	return []call{
		{repeater: 300000301, src: 3000003, dst: 3000001, slot2: true, text: "N0CALL, see you on the net tonight"},
		{repeater: 300000101, src: 3000001, dst: 3000003, slot2: true, text: "Thanks N2CALL, I will be there"},
		{repeater: 300000201, src: 3000002, dst: 3100, group: true, slot2: true, text: "Net control tonight: N1CALL"},
		{repeater: 300000401, src: 3000004, dst: 3000001, slot2: true, text: "N0CALL, the repeater sounds good"},
	}
}

func (r *runner) play(c call) error {
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
	p, ok := r.peers[c.repeater]
	if !ok {
		return fmt.Errorf("repeater %d is not connected", c.repeater)
	}
	if err := r.advance(callSlot); err != nil {
		return err
	}
	start := r.cursor
	r.stream++
	err = p.play(transmission{src: c.src, dst: c.dst, group: c.group, slot2: c.slot2, bursts: bursts}, r.stream, func(k int) error {
		r.cursor = start.Add(time.Duration(k) * burstGap)
		r.c.freeze(r.cursor)
		return nil
	})
	time.Sleep(time.Second)
	r.cursor = r.cursor.Add(time.Second)
	r.c.freeze(r.cursor)
	return err
}

func playCalls(calls []call) hook {
	return func(_ context.Context, r *runner) error {
		for _, c := range calls {
			if err := r.play(c); err != nil {
				return err
			}
			time.Sleep(time.Second)
		}
		return nil
	}
}

func callParrot(_ context.Context, r *runner) error {
	if err := r.play(call{repeater: 300000101, src: 3000001, dst: 9990, group: true, slot2: true, frames: 40}); err != nil {
		return err
	}
	time.Sleep(10 * time.Second)
	return nil
}

type track struct {
	repeater uint32
	radio    uint
	points   [][2]float64
}

func demoTracks() []track {
	return []track{
		{300000101, 3000001, [][2]float64{{32.7767, -96.7970}, {32.7810, -96.7905}, {32.7862, -96.7851}, {32.7921, -96.7812}, {32.7985, -96.7790}}},
		{300000201, 3000002, [][2]float64{{32.9483, -96.7299}, {32.9531, -96.7365}, {32.9580, -96.7428}, {32.9632, -96.7487}, {32.9690, -96.7544}}},
	}
}

func reportPositions(_ context.Context, r *runner) error {
	stream := uint(50000)
	tracks := demoTracks()
	for i := range len(tracks[0].points) {
		if err := r.advance(positionStep); err != nil {
			return err
		}
		for _, t := range tracks {
			b, err := locationReport(t.radio, 5050, t.points[i][0], t.points[i][1], uint32(t.radio))
			if err != nil {
				return err
			}
			stream++
			at := r.cursor
			err = r.peers[t.repeater].play(transmission{src: t.radio, dst: 5050, slot2: true, bursts: b}, stream, func(k int) error {
				r.c.freeze(at.Add(time.Duration(k) * burstGap))
				return nil
			})
			time.Sleep(500 * time.Millisecond)
			r.c.freeze(at.Add(10 * time.Second))
			if err != nil {
				return err
			}
		}
		time.Sleep(time.Second)
	}
	return nil
}

func finishApp(ctx context.Context, r *runner) error {
	if err := r.b.waitFor(ctx, "!location.href.startsWith('"+r.base+"')"); err != nil {
		return err
	}
	back, ok := r.b.requestedURL(appRedirect)
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
		"redirect_uri":  {appRedirect},
		"client_id":     {appOrigin},
		"code_verifier": {appVerifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/api/v1/oauth/token", strings.NewReader(form.Encode()))
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

func repeaterRules(ctx context.Context, r *runner) error {
	return r.api("n0call").do(ctx, http.MethodPatch, "/api/v1/repeaters/300000101/rules", map[string]any{
		"ts2_users": []uint{3000001, 3000002}, "talkgroup_mode": "block", "talkgroups": []uint{91},
	}, nil)
}

func setEmail(ctx context.Context, r *runner) error {
	return r.api("n0call").do(ctx, http.MethodPut, "/api/v1/users/me/email", map[string]string{"email": "n0call@example.com"}, nil)
}

const gatewayID = 300000102

func startDMRGateway(ctx context.Context, r *runner) error {
	if r.o.ext == "" {
		return errors.New("-ext is required: the directory that hack/external/setup.sh builds")
	}
	admin := r.api("n0call")
	var res struct {
		Password string `json:"password"`
	}
	if err := admin.do(ctx, http.MethodPost, "/api/v1/repeaters", map[string]any{"id": gatewayID, "type": "mmdvm"}, &res); err != nil {
		return err
	}
	body := map[string]any{"ts1_static_talkgroups": talkgroupList([]uint{91}), "ts2_static_talkgroups": talkgroupList([]uint{3100})}
	if err := admin.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/repeaters/%d/talkgroups", gatewayID), body, nil); err != nil {
		return err
	}
	start := r.cursor
	r.c.freeze(start)
	dir := filepath.Join(r.o.work, "dmrgateway")
	log, err := os.Create(filepath.Join(r.o.work, "dmrgateway.log"))
	if err != nil {
		return fmt.Errorf("could not make the DMRGateway log: %w", err)
	}
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(r.o.root, "hack", "stress-ext.sh"), "dmrgateway", dir,
		strconv.Itoa(mmdvmPort), strconv.Itoa(openBridgePort), strconv.Itoa(gatewayID), "91", "0", "0")
	cmd.Env = append(os.Environ(), "DMRHUB_STRESS_EXT_DIR="+r.o.ext, "DMRHUB_EXT_CALLSIGN=N0CALL", "DMRHUB_EXT_PASSWORD="+res.Password,
		"DMRHUB_EXT_LOCATION=Austin, TX", "DMRHUB_EXT_DESCRIPTION=DMRGateway hotspot")
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return fmt.Errorf("could not start DMRGateway: %w", err)
	}
	r.stop = append(r.stop, func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		_ = log.Close()
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		var rp struct {
			Connected time.Time `json:"connected_time"`
		}
		if err := admin.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repeaters/%d", gatewayID), nil, &rp); err == nil && !rp.Connected.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("DMRGateway did not log in; its log is %s", filepath.Join(r.o.work, "dmrgateway.log"))
		}
		time.Sleep(500 * time.Millisecond)
	}
	r.cursor = start.Add(time.Minute)
	r.c.freeze(r.cursor)
	time.Sleep(12 * time.Second)
	return nil
}
