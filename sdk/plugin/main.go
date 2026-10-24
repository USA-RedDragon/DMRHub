// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
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
