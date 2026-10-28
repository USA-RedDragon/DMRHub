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

// Command screenshots makes the screenshots of the docs from the shot
// list, hack/screenshots/shots.yaml. hack/screenshots.sh runs it: the
// overlay phase writes the clock patch that the DMRHub build of the
// screenshots uses, the setup phase takes the shots of the setup wizard
// inside a network namespace of its own, the main phase seeds the demo
// network and takes the other shots, and the check phase compares the
// docs with the shot list.
package main

import (
	"bufio"
	"context"
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
	pluginPort     = 62961
	smtpPort       = 62925
	wizardURL      = "http://127.0.0.1:3955"
	startTimeout   = 60 * time.Second
	stopTimeout    = 30 * time.Second
	adminPassword  = "demo-admin-password"
	userPassword   = "demo-user-password"
)

type options struct {
	phase    string
	hub      string
	chromium string
	work     string
	out      string
	list     string
	root     string
	only     string
	radio    string
	ext      string
}

func main() {
	var o options
	flag.StringVar(&o.phase, "phase", "", "overlay, setup, main or check")
	flag.StringVar(&o.hub, "hub", "", "DMRHub binary")
	flag.StringVar(&o.chromium, "chromium", "/usr/bin/chromium", "Chromium binary")
	flag.StringVar(&o.work, "work", "", "work directory")
	flag.StringVar(&o.out, "out", "", "directory of the screenshots")
	flag.StringVar(&o.list, "list", "hack/screenshots/shots.yaml", "shot list")
	flag.StringVar(&o.root, "root", ".", "root of the repository, for the check phase")
	flag.StringVar(&o.only, "only", "", "take only the shots whose names contain this text")
	flag.StringVar(&o.ext, "ext", "", "directory of the external programs that hack/external/setup.sh builds")
	flag.StringVar(&o.radio, "radio", "", "directory of a built dmr-web, for the Browser Radio shots")
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
	if o.phase == "overlay" {
		if o.work == "" {
			return errors.New("-work is required")
		}
		goroot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
		if err != nil {
			return fmt.Errorf("could not find GOROOT: %w", err)
		}
		return writeOverlay(strings.TrimSpace(string(goroot)), o.work)
	}
	l, err := loadList(o.list)
	if err != nil {
		return err
	}
	if o.phase == "check" {
		return checkDocs(o.root, l)
	}
	if o.hub == "" || o.work == "" || o.out == "" {
		return errors.New("-hub, -work and -out are required")
	}
	if err := os.MkdirAll(o.out, 0o750); err != nil {
		return fmt.Errorf("could not make %s: %w", o.out, err)
	}
	c, err := openClock(filepath.Join(o.work, "clock"))
	if err != nil {
		return err
	}
	defer c.close()
	switch o.phase {
	case "setup":
		return runSetup(ctx, o, l, c)
	case "main":
		return runMain(ctx, o, l, c)
	default:
		return fmt.Errorf("unknown phase %q", o.phase)
	}
}

func (l *shotList) status() ([]byte, error) {
	b, err := json.Marshal(map[string]any{"key": l.Basemap.Key, "part_bytes": l.Basemap.PartBytes})
	if err != nil {
		return nil, fmt.Errorf("could not encode the basemap status: %w", err)
	}
	return b, nil
}

func (l *shotList) phase(name, only string) []shot {
	var out []shot
	for _, s := range l.Shots {
		if s.Phase == name && (only == "" || strings.Contains(s.Name, only) || s.Keep) {
			out = append(out, s)
		}
	}
	return out
}

type hubProcess struct {
	cmd    *exec.Cmd
	exited chan struct{}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func startHub(ctx context.Context, o options, l *shotList, logName string) (*hubProcess, error) {
	cmd := exec.CommandContext(ctx, o.hub)
	cmd.Dir = o.work
	cmd.Env = []string{
		"HOME=" + o.work,
		"XDG_CONFIG_HOME=" + filepath.Join(o.work, "xdg"),
		"PATH=" + filepath.Join(o.work, "bin") + ":/usr/bin:/bin",
		"TZ=" + l.TimeZone,
		"NO_COLOR=1",
		clockEnv + "=" + filepath.Join(o.work, "clock"),
		"HTTP_BIND=127.0.0.1",
		fmt.Sprintf("HTTP_PORT=%d", httpPort),
		"DMR_MMDVM_BIND=127.0.0.1",
		fmt.Sprintf("DMR_MMDVM_PORT=%d", mmdvmPort),
		"DMR_OPENBRIDGE_ENABLED=true",
		"DMR_OPENBRIDGE_BIND=127.0.0.1",
		fmt.Sprintf("DMR_OPENBRIDGE_PORT=%d", openBridgePort),
		"DMR_IPSC_ENABLED=true",
		"DMR_IPSC_BIND=127.0.0.1",
		fmt.Sprintf("DMR_IPSC_PORT=%d", ipscPort),
		"DMR_PLUGINS_ENABLED=true",
		"DMR_PLUGINS_BIND=127.0.0.1",
		fmt.Sprintf("DMR_PLUGINS_PORT=%d", pluginPort),
		"METRICS_BIND=127.0.0.1",
		"PPROF_BIND=127.0.0.1",
		"SMTP_ENABLED=true",
		"SMTP_HOST=127.0.0.1",
		fmt.Sprintf("SMTP_PORT=%d", smtpPort),
		"SMTP_FROM=dmrhub@example.org",
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
	h := &hubProcess{cmd: cmd, exited: make(chan struct{})}
	go func() {
		s := bufio.NewScanner(pr)
		for s.Scan() {
			_, _ = fmt.Fprintln(log, ansi.ReplaceAllString(s.Text(), ""))
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
