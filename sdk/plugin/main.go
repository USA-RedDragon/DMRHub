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
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Main runs a plugin's command: it reads the hub's address and
// fingerprint from the -address and -fingerprint flags, or the
// DMRHUB_PLUGIN_ADDRESS and DMRHUB_PLUGIN_FINGERPRINT environment
// variables, and the token from DMRHUB_PLUGIN_TOKEN, then keeps the plugin
// connected with Run until it gets SIGINT or SIGTERM. It exits with status
// 2 when the address or token is missing.
func Main(serve func(context.Context, *Conn) error) {
	o, err := optionsFrom(flag.CommandLine, os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2) //nolint:revive // a command's bad usage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	Run(ctx, o, serve)
}

// errUsage says what a plugin's command needs.
var errUsage = errors.New("set -address (or DMRHUB_PLUGIN_ADDRESS) and DMRHUB_PLUGIN_TOKEN")

func optionsFrom(fs *flag.FlagSet, args []string, getenv func(string) string) (Options, error) {
	address := fs.String("address", getenv("DMRHUB_PLUGIN_ADDRESS"), "address of the hub's plugin listener, as host:port")
	fingerprint := fs.String("fingerprint", getenv("DMRHUB_PLUGIN_FINGERPRINT"),
		"SHA-256 of the hub's own certificate, from its Plugins page; empty for a certificate from a certificate authority")
	if err := fs.Parse(args); err != nil {
		return Options{}, fmt.Errorf("bad arguments: %w", err)
	}
	o := Options{Address: *address, Token: getenv("DMRHUB_PLUGIN_TOKEN"), Fingerprint: *fingerprint}
	if o.Address == "" || o.Token == "" {
		return Options{}, errUsage
	}
	return o, nil
}
