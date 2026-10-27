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

// Command screenshots runs a DMRHub on loopback with demo data and takes
// the screenshots of the docs with headless Chromium. hack/screenshots.sh
// runs it: first the setup phase inside a network namespace of its own,
// as the setup wizard always listens on port 3005 of every address, then
// the main phase.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	httpPort       = 3955
	mmdvmPort      = 62951
	openBridgePort = 62955
	ipscPort       = 50951
	wizardURL      = "http://localhost:3005"
	startTimeout   = 60 * time.Second
	stopTimeout    = 30 * time.Second
)

type options struct {
	phase    string
	hub      string
	chromium string
	work     string
	out      string
}

// creds is the account that the setup phase makes, for the main phase.
type creds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func main() {
	var o options
	flag.StringVar(&o.phase, "phase", "", "setup or main")
	flag.StringVar(&o.hub, "hub", "", "DMRHub binary")
	flag.StringVar(&o.chromium, "chromium", "/usr/bin/chromium", "Chromium binary")
	flag.StringVar(&o.work, "work", "", "work directory")
	flag.StringVar(&o.out, "out", "", "directory of the screenshots")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, o)
	stop()
	if err != nil {
		slog.Error("Screenshots failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	if o.hub == "" || o.work == "" || o.out == "" {
		return errors.New("-hub, -work and -out are required")
	}
	if err := os.MkdirAll(o.out, 0o750); err != nil {
		return fmt.Errorf("could not make %s: %w", o.out, err)
	}
	switch o.phase {
	case "setup":
		return runSetup(ctx, o)
	case "main":
		return runMain(ctx, o)
	default:
		return fmt.Errorf("unknown phase %q", o.phase)
	}
}

// hubProcess is a DMRHub that the harness started.
type hubProcess struct {
	cmd    *exec.Cmd
	token  chan string
	exited chan struct{}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)
var tokenLine = regexp.MustCompile(`/setup\?token=([A-Za-z0-9]+)`)

// startHub starts DMRHub in the work directory, with its configuration in
// the work directory only and no way to open a desktop browser.
func startHub(ctx context.Context, o options, logName string) (*hubProcess, error) {
	cmd := exec.CommandContext(ctx, o.hub)
	cmd.Dir = o.work
	cmd.Env = []string{
		"HOME=" + o.work,
		"XDG_CONFIG_HOME=" + filepath.Join(o.work, "xdg"),
		"PATH=" + filepath.Join(o.work, "bin") + ":/usr/bin:/bin",
		"TZ=America/Chicago",
		"NO_COLOR=1",
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopTimeout
	log, err := os.Create(filepath.Join(o.work, logName))
	if err != nil {
		return nil, fmt.Errorf("could not make the DMRHub log: %w", err)
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start DMRHub: %w", err)
	}
	h := &hubProcess{cmd: cmd, token: make(chan string, 1), exited: make(chan struct{})}
	go func() {
		s := bufio.NewScanner(pr)
		for s.Scan() {
			line := ansi.ReplaceAllString(s.Text(), "")
			if m := tokenLine.FindStringSubmatch(line); m != nil {
				select {
				case h.token <- m[1]:
				default:
				}
				line = tokenLine.ReplaceAllString(line, "/setup?token=<token>")
			}
			_, _ = fmt.Fprintln(log, line)
		}
		_ = log.Close()
	}()
	go func() {
		_ = cmd.Wait()
		_ = pw.Close()
		close(h.exited)
	}()
	return h, nil
}

// stop ends DMRHub with SIGTERM and waits for it.
func (h *hubProcess) stop() {
	select {
	case <-h.exited:
		return
	default:
	}
	_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-h.exited:
	case <-time.After(stopTimeout):
		_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGKILL)
		<-h.exited
	}
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("no randomness: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func credsFile(o options) string {
	return filepath.Join(o.work, "creds.json")
}

// runSetup takes the setup wizard screenshots and finishes the setup with
// the configuration of the main phase and the first admin, N0CALL.
func runSetup(ctx context.Context, o options) error {
	hub, err := startHub(ctx, o, "setup.log")
	if err != nil {
		return err
	}
	defer hub.stop()
	var token string
	select {
	case token = <-hub.token:
	case <-hub.exited:
		return errors.New("DMRHub stopped before its setup wizard started")
	case <-time.After(startTimeout):
		return fmt.Errorf("setup wizard: %w", errTimeout)
	}
	wizard := newAPI(wizardURL)
	wizard.header = map[string]string{"X-SetupWizard-Token": token}
	if err := wizard.waitUp(ctx); err != nil {
		return err
	}
	b, err := startBrowser(ctx, o.chromium, o.work)
	if err != nil {
		return err
	}
	defer b.close()

	if err := b.navigate(ctx, wizardURL+"/setup?token="+token); err != nil {
		return err
	}
	if err := b.waitText(ctx, "General Settings"); err != nil {
		return err
	}
	if err := b.shot(ctx, filepath.Join(o.out, "setup.png")); err != nil {
		return err
	}
	if err := b.scrollTo(ctx, "DMR Settings"); err != nil {
		return err
	}
	if err := b.shot(ctx, filepath.Join(o.out, "setup-dmr.png")); err != nil {
		return err
	}

	if err := configure(ctx, wizard); err != nil {
		return err
	}
	password, err := randomString(12)
	if err != nil {
		return err
	}
	c := creds{Username: "n0call", Password: password}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("could not encode the account: %w", err)
	}
	if err := os.WriteFile(credsFile(o), data, 0o600); err != nil {
		return fmt.Errorf("could not save the account: %w", err)
	}

	if err := b.navigate(ctx, wizardURL+"/setup/user?token="+token); err != nil {
		return err
	}
	for _, f := range [][2]string{{"#dmr_id", "3000001"}, {"#username", c.Username}, {"#callsign", "N0CALL"}, {"#password", password}, {"#confirmPassword", password}} {
		if err := b.typeInto(ctx, f[0], f[1]); err != nil {
			return err
		}
	}
	if err := b.eval(ctx, "document.activeElement.blur()", nil); err != nil {
		return err
	}
	if err := b.shot(ctx, filepath.Join(o.out, "setup-user.png")); err != nil {
		return err
	}
	if err := b.click(ctx, "Create User"); err != nil {
		return err
	}
	hub2 := newAPI(fmt.Sprintf("http://127.0.0.1:%d", httpPort))
	if err := hub2.waitUp(ctx); err != nil {
		return fmt.Errorf("DMRHub did not start after the setup: %w", err)
	}
	return nil
}

// configure saves the configuration of the demo network through the
// setup wizard API: everything on loopback, SQLite, and no outside
// services.
func configure(ctx context.Context, wizard *api) error {
	var cfg map[string]any
	if err := wizard.do(ctx, "GET", "/api/v1/config", nil, &cfg); err != nil {
		return err
	}
	delete(cfg, "secrets")
	secret, err := randomString(24)
	if err != nil {
		return err
	}
	salt, err := randomString(24)
	if err != nil {
		return err
	}
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
	set("secret", secret)
	set("password-salt", salt)
	set("network-name", "DMRHub Demo")
	set("http.bind", "127.0.0.1")
	set("http.port", httpPort)
	set("http.canonical-host", fmt.Sprintf("http://127.0.0.1:%d", httpPort))
	set("dmr.mmdvm.bind", "127.0.0.1")
	set("dmr.mmdvm.port", mmdvmPort)
	set("dmr.openbridge.enabled", true)
	set("dmr.openbridge.bind", "127.0.0.1")
	set("dmr.openbridge.port", openBridgePort)
	set("dmr.ipsc.enabled", true)
	set("dmr.ipsc.bind", "127.0.0.1")
	set("dmr.ipsc.port", ipscPort)
	set("dmr.ipsc.network-id", 300099)
	set("dmr.disable-radio-id-validation", true)
	set("dmr.radio-id-url", "http://127.0.0.1:9/users.json")
	set("dmr.repeater-id-url", "http://127.0.0.1:9/rptrs.json")
	set("dmr.plugins.bind", "127.0.0.1")
	set("metrics.bind", "127.0.0.1")
	set("pprof.bind", "127.0.0.1")
	return wizard.do(ctx, "PUT", "/api/v1/config", cfg, nil)
}

func readCreds(o options) (creds, error) {
	var c creds
	data, err := os.ReadFile(credsFile(o))
	if err != nil {
		return c, fmt.Errorf("run the setup phase first: %w", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("bad account file: %w", err)
	}
	return c, nil
}
