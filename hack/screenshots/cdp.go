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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

const (
	viewWidth   = 1280
	viewHeight  = 800
	scale       = 2
	cdpTimeout  = 30 * time.Second
	readLimit   = 128 << 20
	devToolsMax = 20 * time.Second
)

var errTimeout = errors.New("timed out")

type browser struct {
	cmd       *exec.Cmd
	conn      *websocket.Conn
	port      string
	pending   map[string]string
	requested []string
	target    string
	at        time.Time
	timeZone  string
	status    []byte
	secrets   *secrets
	scans     sync.WaitGroup

	mu      sync.Mutex
	nextID  int
	waiting map[int]chan cdpReply
	dialogs []string
}

type cdpReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type cdpMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	cdpReply
}

func startBrowser(ctx context.Context, chromium, dir, timeZone string, status []byte, sec *secrets) (*browser, error) {
	profile := filepath.Join(dir, "chromium")
	if err := os.RemoveAll(profile); err != nil {
		return nil, fmt.Errorf("could not clear the profile: %w", err)
	}
	args := []string{
		"--headless=new", "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check",
		"--hide-scrollbars", "--force-color-profile=srgb", "--disable-extensions", "--mute-audio",
		"--disable-background-networking", "--disable-sync", "--disable-component-update",
		"--lang=en-US", "--no-sandbox", fmt.Sprintf("--window-size=%d,%d", viewWidth, viewHeight), "about:blank",
	}
	cmd := exec.CommandContext(ctx, chromium, args...)
	log, err := os.Create(filepath.Join(dir, "chromium.log"))
	if err != nil {
		return nil, fmt.Errorf("could not make the Chromium log: %w", err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(os.Environ(), "TZ="+timeZone, "LANG=en_US.UTF-8")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start Chromium: %w", err)
	}
	b := &browser{cmd: cmd, timeZone: timeZone, status: status, secrets: sec}
	port, err := devToolsPort(profile)
	if err != nil {
		b.close()
		return nil, err
	}
	b.port = port
	if err := b.openPage(ctx); err != nil {
		b.close()
		return nil, err
	}
	return b, nil
}

func (b *browser) openPage(ctx context.Context) error {
	if b.conn != nil {
		_ = b.conn.Close(websocket.StatusNormalClosure, "")
		b.conn = nil
		if _, err := b.devTools(ctx, http.MethodGet, "/json/close/"+b.target); err != nil {
			return err
		}
	}
	body, err := b.devTools(ctx, http.MethodPut, "/json/new?about:blank")
	if err != nil {
		return err
	}
	var t struct {
		ID  string `json:"id"`
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return fmt.Errorf("bad new page: %w", err)
	}
	conn, _, err := websocket.Dial(ctx, t.URL, nil)
	if err != nil {
		return fmt.Errorf("could not reach the page: %w", err)
	}
	conn.SetReadLimit(readLimit)
	b.mu.Lock()
	b.conn, b.target, b.waiting, b.pending = conn, t.ID, map[int]chan cdpReply{}, map[string]string{}
	b.mu.Unlock()
	go b.read(ctx, conn)
	return b.setup(ctx)
}

func (b *browser) devTools(ctx context.Context, method, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:"+b.port+path, nil)
	if err != nil {
		return nil, fmt.Errorf("bad request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no DevTools: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("no DevTools: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DevTools %s %s: %s", method, path, resp.Status)
	}
	return data, nil
}

func devToolsPort(profile string) (string, error) {
	deadline := time.Now().Add(devToolsMax)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			return strings.SplitN(string(b), "\n", 2)[0], nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("chromium did not open DevTools: %w", errTimeout)
}

func (b *browser) setup(ctx context.Context) error {
	steps := []struct {
		method string
		params any
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Network.enable", nil},
		{"Network.setCacheDisabled", map[string]any{"cacheDisabled": true}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": viewWidth, "height": viewHeight, "deviceScaleFactor": scale, "mobile": false}},
		{"Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": "light"}}}},
		{"Emulation.setTimezoneOverride", map[string]any{"timezoneId": b.timeZone}},
		{"Emulation.setLocaleOverride", map[string]any{"locale": "en-US"}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]string{{"urlPattern": tilesStatus + "*"}}}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": pageScript(b.at)}},
	}
	for _, s := range steps {
		if _, err := b.call(ctx, s.method, s.params); err != nil {
			return err
		}
	}
	return nil
}

func (b *browser) read(ctx context.Context, conn *websocket.Conn) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m cdpMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID != 0 {
			b.mu.Lock()
			ch := b.waiting[m.ID]
			delete(b.waiting, m.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- m.cdpReply
			}
			continue
		}
		b.event(ctx, m)
	}
}

func (b *browser) event(ctx context.Context, m cdpMessage) {
	switch m.Method {
	case "Network.requestWillBeSent":
		var p struct {
			ID      string `json:"requestId"`
			Request struct {
				URL string `json:"url"`
			} `json:"request"`
		}
		_ = json.Unmarshal(m.Params, &p)
		b.mu.Lock()
		if !strings.Contains(p.Request.URL, "worker") {
			b.pending[p.ID] = time.Now().Format("15:04:05.000") + " " + p.Request.URL
		}
		b.requested = append(b.requested, p.Request.URL)
		b.mu.Unlock()
	case "Network.responseReceived":
		var p struct {
			ID   string `json:"requestId"`
			Type string `json:"type"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.Type == "Document" {
			b.mu.Lock()
			delete(b.pending, p.ID)
			b.mu.Unlock()
		}
	case "Network.loadingFinished":
		var p struct {
			ID string `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		b.mu.Lock()
		u := b.pending[p.ID]
		delete(b.pending, p.ID)
		b.mu.Unlock()
		if strings.Contains(u, "/api/") {
			b.scans.Add(1)
			go func() {
				defer b.scans.Done()
				b.scanBody(ctx, p.ID)
			}()
		}
	case "Network.loadingFailed":
		var p struct {
			ID string `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		b.mu.Lock()
		delete(b.pending, p.ID)
		b.mu.Unlock()
		slog.Debug("Request failed", "detail", string(m.Params))
	case "Fetch.requestPaused":
		var p struct {
			ID string `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		go func() {
			_, err := b.call(ctx, "Fetch.fulfillRequest", map[string]any{
				"requestId":    p.ID,
				"responseCode": http.StatusOK,
				"responseHeaders": []map[string]string{
					{"name": "Content-Type", "value": "application/json"},
					{"name": "Access-Control-Allow-Origin", "value": "*"},
				},
				"body": base64.StdEncoding.EncodeToString(b.status),
			})
			if err != nil {
				slog.Warn("Could not answer the basemap status", "error", err)
			}
		}()
	case "Runtime.exceptionThrown":
		slog.Warn("Page error", "detail", string(m.Params))
	case "Page.javascriptDialogOpening":
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(m.Params, &p)
		b.mu.Lock()
		b.dialogs = append(b.dialogs, p.Message)
		b.mu.Unlock()
		go func() {
			if _, err := b.call(ctx, "Page.handleJavaScriptDialog", map[string]any{"accept": true}); err != nil {
				slog.Warn("Could not close a dialog", "error", err)
			}
		}()
	}
}

func (b *browser) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	ch := make(chan cdpReply, 1)
	b.waiting[id] = ch
	b.mu.Unlock()
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	cctx, cancel := context.WithTimeout(ctx, cdpTimeout)
	defer cancel()
	if err := b.conn.Write(cctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	case <-cctx.Done():
		return nil, fmt.Errorf("%s: %w", method, errTimeout)
	}
}

func (b *browser) navigate(ctx context.Context, url string) error {
	if err := b.openPage(ctx); err != nil {
		return err
	}
	if _, err := b.call(ctx, "Page.navigate", map[string]any{"url": url}); err != nil {
		return err
	}
	return b.waitFor(ctx, "location.href !== 'about:blank' && document.readyState === 'complete'")
}

func (b *browser) eval(ctx context.Context, expression string, out any) error {
	res, err := b.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true, "userGesture": true})
	if err != nil {
		return err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return fmt.Errorf("bad evaluate answer: %w", err)
	}
	if r.Exception != nil {
		return fmt.Errorf("script error: %s %s", r.Exception.Text, r.Exception.Exception.Description)
	}
	if out != nil && len(r.Result.Value) > 0 {
		if err := json.Unmarshal(r.Result.Value, out); err != nil {
			return fmt.Errorf("bad script value: %w", err)
		}
	}
	return nil
}

func (b *browser) waitFor(ctx context.Context, expression string) error {
	deadline := time.Now().Add(cdpTimeout)
	for {
		var ok bool
		if err := b.eval(ctx, "Boolean("+expression+")", &ok); err == nil && ok {
			return nil
		}
		if time.Now().After(deadline) {
			var where string
			_ = b.eval(ctx, "location.href + ' | ' + (document.body ? document.body.innerText.slice(0, 300) : '')", &where)
			b.mu.Lock()
			for _, u := range b.pending {
				where += "\npending: " + u
			}
			b.mu.Unlock()
			return fmt.Errorf("waiting for %s on %q: %w", expression, where, errTimeout)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func (b *browser) waitText(ctx context.Context, text string) error {
	q, err := json.Marshal(text)
	if err != nil {
		return fmt.Errorf("bad text: %w", err)
	}
	return b.waitFor(ctx, "document.body && document.body.innerText.includes("+string(q)+")")
}

func (b *browser) click(ctx context.Context, text string) error {
	t, err := json.Marshal(text)
	if err != nil {
		return fmt.Errorf("bad text: %w", err)
	}
	find := "[...document.querySelectorAll('button')].find((e) => e.innerText.includes(" + string(t) + "))"
	if err := b.waitFor(ctx, find); err != nil {
		return err
	}
	return b.eval(ctx, "("+find+").click()", nil)
}

func (b *browser) scrollTo(ctx context.Context, title string) error {
	t, err := json.Marshal(title)
	if err != nil {
		return fmt.Errorf("bad title: %w", err)
	}
	find := "[...document.querySelectorAll('body *')].find((e) => e.children.length === 0 && e.textContent.trim() === " + string(t) + ")"
	if err := b.waitFor(ctx, find); err != nil {
		return err
	}
	return b.eval(ctx, `(() => {
		const e = `+find+`;
		const card = e.closest('[data-slot=card]') || e;
		const header = document.querySelector('header');
		const top = header ? header.getBoundingClientRect().height : 0;
		window.scrollTo(0, card.getBoundingClientRect().top + window.scrollY - top - 16);
	})()`, nil)
}

func (b *browser) clickTest(ctx context.Context, test string) error {
	q, err := json.Marshal("[data-test=\"" + test + "\"]")
	if err != nil {
		return fmt.Errorf("bad selector: %w", err)
	}
	if err := b.waitFor(ctx, "document.querySelector("+string(q)+")"); err != nil {
		return err
	}
	return b.eval(ctx, "document.querySelector("+string(q)+").click()", nil)
}

const tilesStatus = "https://tiles.squallar.app/status/"

const quietTime = 1500 * time.Millisecond

func pageScript(at time.Time) string {
	return fmt.Sprintf(`(() => {
	try { localStorage.setItem('color-mode', 'light'); } catch (e) {}
	const T = %d;
	const D = Date;
	function F(...a) {
		if (!new.target) return new D(T).toString();
		return a.length ? new D(...a) : new D(T);
	}
	F.prototype = D.prototype;
	F.now = () => T;
	F.parse = D.parse;
	F.UTC = D.UTC;
	Object.defineProperty(F, 'name', { value: 'Date' });
	globalThis.Date = F;
	let seed = 0x9e3779b9;
	const next = () => { seed ^= seed << 13; seed ^= seed >>> 17; seed ^= seed << 5; return (seed >>> 0) / 4294967296; };
	Math.random = next;
	if (globalThis.crypto) {
		crypto.getRandomValues = (a) => { const b = new Uint8Array(a.buffer, a.byteOffset, a.byteLength); for (let i = 0; i < b.length; i++) b[i] = Math.floor(next() * 256); return a; };
		crypto.randomUUID = () => '00000000-0000-4000-8000-' + String(Math.floor(next() * 1e12)).padStart(12, '0');
	}
	const still = () => {
		const s = document.createElement('style');
		s.textContent = '*, *::before, *::after { transition: none !important; animation: none !important; caret-color: transparent !important; }';
		document.documentElement.appendChild(s);
	};
	if (document.documentElement) still(); else document.addEventListener('DOMContentLoaded', still);
})()`, at.UnixMilli())
}

func (b *browser) idle(ctx context.Context) error {
	deadline := time.Now().Add(cdpTimeout)
	quiet := time.Now()
	for time.Now().Before(deadline) {
		b.mu.Lock()
		n := len(b.pending)
		b.mu.Unlock()
		if n > 0 {
			quiet = time.Now()
		} else if time.Since(quiet) >= quietTime {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the page: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	b.mu.Lock()
	var urls []string
	for _, u := range b.pending {
		urls = append(urls, u)
	}
	b.mu.Unlock()
	return fmt.Errorf("the page still loads %s: %w", strings.Join(urls, ", "), errTimeout)
}

func (b *browser) settle(ctx context.Context) error {
	if err := b.eval(ctx, "document.fonts.ready.then(() => true)", nil); err != nil {
		return err
	}
	if err := b.idle(ctx); err != nil {
		return err
	}
	var last string
	stable := 0
	deadline := time.Now().Add(cdpTimeout)
	for stable < 3 && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		var now string
		if err := b.eval(ctx, "document.body.innerText + ':' + document.querySelectorAll('*').length", &now); err != nil {
			return err
		}
		if now == last {
			stable++
		} else {
			stable = 0
		}
		last = now
	}
	if err := b.eval(ctx, "new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))", nil); err != nil {
		return err
	}
	return b.idle(ctx)
}

func (b *browser) scanBody(ctx context.Context, id string) {
	res, err := b.call(ctx, "Network.getResponseBody", map[string]any{"requestId": id})
	if err != nil {
		return
	}
	var r struct {
		Body   string `json:"body"`
		Base64 bool   `json:"base64Encoded"`
	}
	if json.Unmarshal(res, &r) != nil || r.Base64 {
		return
	}
	var v any
	if json.Unmarshal([]byte(r.Body), &v) == nil {
		b.secrets.scan(v)
	}
}

func (b *browser) waitSecrets() {
	b.scans.Wait()
}

func (b *browser) mask(ctx context.Context, secrets map[string]string) error {
	if len(secrets) == 0 {
		return nil
	}
	m, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("bad mask: %w", err)
	}
	return b.eval(ctx, `(() => {
		const m = `+string(m)+`;
		const swap = (t) => { for (const [k, v] of Object.entries(m)) t = t.split(k).join(v); return t; };
		const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
		for (let n = w.nextNode(); n; n = w.nextNode()) n.nodeValue = swap(n.nodeValue);
		for (const e of document.querySelectorAll('input:not([type=file]), textarea')) { const v = swap(e.value); if (v !== e.value) e.value = v; }
	})()`, nil)
}

func (b *browser) rect(ctx context.Context, clip string) (map[string]float64, error) {
	kind, arg, _ := strings.Cut(clip, " ")
	a, err := json.Marshal(arg)
	if err != nil {
		return nil, fmt.Errorf("bad clip: %w", err)
	}
	var find string
	switch kind {
	case "card":
		find = "(() => { const t = [...document.querySelectorAll('[data-slot=card] *')].find((e) => e.children.length === 0 && e.textContent.trim() === " + string(a) + "); return t && t.closest('[data-slot=card]'); })()"
	case "dialog":
		find = "document.querySelector('[role=dialog], [role=alertdialog]')"
	case "css":
		find = "document.querySelector(" + string(a) + ")"
	default:
		return nil, fmt.Errorf("unknown clip %q", clip)
	}
	if err := b.waitFor(ctx, find); err != nil {
		return nil, err
	}
	if err := b.eval(ctx, "("+find+").scrollIntoView({block: 'center'})", nil); err != nil {
		return nil, err
	}
	if err := b.settle(ctx); err != nil {
		return nil, err
	}
	var r map[string]float64
	if err := b.eval(ctx, `(() => {
		const r = (`+find+`).getBoundingClientRect();
		const p = 8;
		const x = Math.max(0, Math.floor(r.left + scrollX - p)), y = Math.max(0, Math.floor(r.top + scrollY - p));
		return { x, y, width: Math.ceil(r.width + 2 * p), height: Math.ceil(r.height + 2 * p), scale: 1 };
	})()`, &r); err != nil {
		return nil, err
	}
	return r, nil
}

func (b *browser) shot(ctx context.Context, file, clip string) error {
	if err := b.settle(ctx); err != nil {
		return err
	}
	params := map[string]any{"format": "png"}
	if clip != "" {
		r, err := b.rect(ctx, clip)
		if err != nil {
			return err
		}
		params["clip"] = r
		params["captureBeyondViewport"] = true
	}
	res, err := b.call(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return err
	}
	var r struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return fmt.Errorf("bad screenshot: %w", err)
	}
	png, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return fmt.Errorf("bad screenshot: %w", err)
	}
	if err := os.WriteFile(file, png, 0o644); err != nil {
		return fmt.Errorf("could not write %s: %w", file, err)
	}
	slog.Info("Shot", "file", filepath.Base(file))
	return nil
}

func (b *browser) setFiles(ctx context.Context, selector, file string) error {
	q, err := json.Marshal(selector)
	if err != nil {
		return fmt.Errorf("bad selector: %w", err)
	}
	if err := b.waitFor(ctx, "document.querySelector("+string(q)+")"); err != nil {
		return err
	}
	res, err := b.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.querySelector(" + string(q) + ")"})
	if err != nil {
		return err
	}
	var r struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return fmt.Errorf("bad evaluate answer: %w", err)
	}
	_, err = b.call(ctx, "DOM.setFileInputFiles", map[string]any{"files": []string{file}, "objectId": r.Result.ObjectID})
	return err
}

func (b *browser) requestedURL(prefix string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.requested) - 1; i >= 0; i-- {
		if strings.HasPrefix(b.requested[i], prefix) {
			return b.requested[i], true
		}
	}
	return "", false
}

func (b *browser) close() {
	if b.conn != nil {
		_ = b.conn.Close(websocket.StatusNormalClosure, "")
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
		_ = b.cmd.Wait()
	}
}
