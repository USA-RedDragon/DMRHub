// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

//nolint:golint,gomnd
package client

import (
	"fmt"
	"os"
	"syscall"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/ztrue/shutdown"
)

func main() { //nolint:golint,unused
	c, err := NewClient(&models.RepeaterConfiguration{
		ID:          319186801,
		Callsign:    "KI5VMF",
		RXFrequency: 443000000,
		TXFrequency: 443500000,
		TXPower:     25,
		ColorCode:   1,
		Latitude:    32.7157,
		Longitude:   -117.1611,
		Height:      0,
		Location:    "San Diego, CA",
		Description: "Test Repeater",
		Slots:       1,
		URL:         "http://www.ki5vmf.com",
	}, "127.0.0.1", "password")

	if err != nil {
		panic(err)
	}

	packets := make(chan *models.Packet, 100)
	ch := make(chan string)

	err = c.ListenAndServe(packets)
	if err != nil {
		panic(err)
	}

	stop := func(sig os.Signal) {
		c.Close()
		close(ch)
	}

	defer stop(syscall.SIGINT)
	shutdown.AddWithParam(stop)
	shutdown.Listen(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	for {
		select {
		case packet := <-packets:
			fmt.Println(packet)
		case <-ch:
			fmt.Println("Client closed")
		}
	}
}
