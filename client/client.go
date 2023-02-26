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

//nolint:golint,wrapcheck
package client

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
)

type authState uint8

const (
	authNone authState = iota
	authSentLogin
	authSentKey
	authDone
	authFailed
)

// Client implementing the Homebrew protocol
type Client struct {
	KeepAlive time.Duration
	Timeout   time.Duration

	// Configuration of the repeater
	Configuration *models.RepeaterConfiguration

	// Description of the client
	Description string

	// URL of the client
	URL string

	conn net.Conn
	data chan []byte
	quit chan struct{}
	errs chan error

	password    string
	auth        authState
	hexid       [4]byte
	nonce       [4]byte
	pingSent    time.Time
	pingLatency time.Duration
}

const (
	DefaultPort              = 62031
	DefaultKeepAliveInterval = time.Second * 10
	DefaultTimeout           = time.Second * 15
)

var (
	ErrMasterRefusedLogin    = errors.New("homebrew: master refused login")
	ErrMasterRefusedPassword = errors.New("homebrew: master refused password")
	ErrMasterClose           = errors.New("homebrew: master sent close")
	ErrMasterShortNonce      = errors.New("homebrew: master sent short nonce")
	ErrNilConfiguration      = errors.New("homebrew: nil configuration")
	ErrTimeout               = errors.New("homebrew: master connection timeout")
)

// NewClient sets up a Homebrew protocol client with defaults configured.
func NewClient(cfg *models.RepeaterConfiguration, addr, password string) (*Client, error) {
	if cfg == nil {
		return nil, ErrNilConfiguration
	}
	if err := cfg.Check(); err != nil {
		return nil, err
	}

	c := &Client{
		KeepAlive:     DefaultKeepAliveInterval,
		Timeout:       DefaultTimeout,
		Configuration: cfg,
		password:      password,
	}

	c.hexid[0] = byte(c.Configuration.ID >> 24) //nolint:golint,gomnd
	c.hexid[1] = byte(c.Configuration.ID >> 16) //nolint:golint,gomnd
	c.hexid[2] = byte(c.Configuration.ID >> 8)  //nolint:golint,gomnd
	c.hexid[3] = byte(c.Configuration.ID)

	if !strings.Contains(addr, ":") {
		addr = fmt.Sprintf("%s:%d", addr, DefaultPort)
	}

	log.Printf("homebrew: connecting to udp://%s\n", addr)

	var err error
	if c.conn, err = net.Dial("udp", addr); err != nil {
		return nil, err
	}

	return c, nil
}

// Close the client socket and stop the receiver loop after it has been started
// by ListenAndServe.
func (c *Client) Close() error {
	if c.quit != nil {
		c.quit <- struct{}{} // listen
		c.quit <- struct{}{} // receive
	}
	return c.conn.Close()
}

// ListenAndServe starts the packet receiver and decoder
func (c *Client) ListenAndServe(f chan<- *models.Packet) error {
	c.quit = make(chan struct{}, 2) //nolint:golint,gomnd
	c.data = make(chan []byte)
	c.errs = make(chan error)

	go c.receive()

	if err := c.sendLogin(); err != nil {
		return err
	}

	var (
		timeout = time.NewTicker(c.Timeout)
	)

	for {
		if c.auth == authDone && time.Since(c.pingSent) > c.KeepAlive {
			if err := c.sendPing(); err != nil {
				return err
			}
		}

		select {
		case data := <-c.data:
			if err := c.parse(data, f); err != nil {
				c.quit <- struct{}{} // signal receiver
				return err
			}

			timeout.Stop()
			timeout = time.NewTicker(c.Timeout)

		case <-time.After(c.KeepAlive):
			if c.auth == authDone {
				if err := c.sendPing(); err != nil {
					c.quit <- struct{}{} // signal receiver
					return err
				}
			}

		case <-timeout.C:
			c.quit <- struct{}{} // signal receiver
			return ErrTimeout

		case <-c.quit:
			return nil

		case err := <-c.errs:
			return err
		}
	}
}

// SendDMR sends a DMRData packet
func (c *Client) SendDMR(dmrData *models.Packet) error {
	_, err := c.conn.Write(dmrData.Encode())
	return err
}

func (c *Client) parse(b []byte, f chan<- *models.Packet) error {
	const minPacketLen = 3
	if len(b) < minPacketLen {
		return io.ErrShortBuffer
	}

	switch c.auth { //nolint:golint,exhaustive
	case authSentLogin:
		// We expect RPTACK or MSTNAK
		if bytes.Equal(b[:len(dmrconst.CommandMSTNAK)], []byte(dmrconst.CommandMSTNAK)) {
			c.auth = authFailed
			return ErrMasterRefusedLogin
		} else if bytes.Equal(b[:len(dmrconst.CommandRPTACK)], []byte(dmrconst.CommandRPTACK)) {
			if n := copy(c.nonce[:], b[len(dmrconst.CommandRPTACK):]); n != len(c.nonce) {
				c.auth = authFailed
				logging.Errorf("homebrew: received short nonce: %d", n)
				return ErrMasterShortNonce
			}
			log.Println("homebrew: received nonce, sending password")
			return c.sendKey()
		}

		// Ignored
		log.Printf("homebrew: %q\n", b)
		return nil

	case authSentKey:
		// We expect RPTACK or MSTNAK
		if bytes.Equal(b[:len(dmrconst.CommandMSTNAK)], []byte(dmrconst.CommandMSTNAK)) {
			c.auth = authFailed
			return ErrMasterRefusedPassword
		} else if bytes.Equal(b[:len(dmrconst.CommandRPTACK)], []byte(dmrconst.CommandRPTACK)) {
			log.Println("homebrew: logged in, sending configuration")
			c.auth = authDone
			return c.sendConfiguration()
		}

		// Ignored
		log.Printf("homebrew: %q\n", b)
		return nil
	}

	switch {
	case bytes.Equal(b[:len(dmrconst.CommandDMRD)], []byte(dmrconst.CommandDMRD)):
		var dmrData models.Packet
		dmrData, ok := models.UnpackPacket(b)
		if !ok {
			log.Print("homebrew: failed to decode DMRD\n")
			return nil
		}
		f <- &dmrData
		return nil

	case bytes.Equal(b[:len(dmrconst.CommandMSTCL)], []byte(dmrconst.CommandMSTCL)):
		return ErrMasterClose

	case bytes.Equal(b[:len(dmrconst.CommandRPTACK)], []byte(dmrconst.CommandRPTACK)):
		log.Println("homebrew: configuration accepted by master")
		return c.sendPing()

	case bytes.Equal(b[:len(dmrconst.CommandMSTNAK)], []byte(dmrconst.CommandMSTNAK)):
		log.Println("homebrew: master dropped connection, logging in")
		return c.sendLogin()

	case bytes.Equal(b[:len(dmrconst.CommandMSTPONG)], []byte(dmrconst.CommandMSTPONG)):
		c.pingLatency = time.Since(c.pingSent)
		log.Printf("homebrew: ping RTT %s\n", c.pingLatency)
		return nil
	}

	log.Printf("homebrew: %q\n", b)

	return nil
}

func (c *Client) receive() {
	const bufferSize = 128 // bytes
	for {
		data := make([]byte, bufferSize)
		n, err := c.conn.Read(data)
		if err != nil {
			c.errs <- err
			return
		}
		c.data <- data[:n]
	}
}

func (c *Client) sendLogin() error {
	var (
		data = make([]byte, len(dmrconst.CommandRPTL)+len(c.hexid))
		n    = copy(data, dmrconst.CommandRPTL)
	)
	copy(data[n:], c.hexid[:])
	c.auth = authSentLogin
	_, err := c.conn.Write(data)
	return err
}

func (c *Client) sendKey() error {
	var (
		hash = sha256.Sum256(append(c.nonce[:], []byte(c.password)...))
		data = make([]byte, len(dmrconst.CommandRPTK)+len(hash)+len(c.hexid))
		n    = copy(data, dmrconst.CommandRPTK)
	)
	n += copy(data[n:], c.hexid[:])
	copy(data[n:], hash[:4])

	c.auth = authSentKey
	_, err := c.conn.Write(data)
	return err
}

func (c *Client) sendConfiguration() error {
	if err := c.Configuration.Check(); err != nil {
		return err
	}
	var data []byte
	data = []byte(dmrconst.CommandRPTC)
	data = append(data, c.hexid[:]...)
	data = append(data, []byte(fmt.Sprintf("%-8s", c.Configuration.Callsign))...)
	data = append(data, []byte(fmt.Sprintf("%09d", c.Configuration.RXFrequency))...)
	data = append(data, []byte(fmt.Sprintf("%09d", c.Configuration.TXFrequency))...)
	data = append(data, []byte(fmt.Sprintf("%02d", c.Configuration.TXPower))...)
	data = append(data, []byte(fmt.Sprintf("%02d", c.Configuration.ColorCode))...)
	data = append(data, []byte(fmt.Sprintf("%-08f", c.Configuration.Latitude)[:8])...)
	data = append(data, []byte(fmt.Sprintf("%-09f", c.Configuration.Longitude)[:9])...)
	data = append(data, []byte(fmt.Sprintf("%03d", c.Configuration.Height))...)
	data = append(data, []byte(fmt.Sprintf("%-20s", c.Configuration.Location))...)
	data = append(data, []byte(fmt.Sprintf("%-20s", c.Configuration.Description))...)
	data = append(data, []byte(fmt.Sprintf("%d", c.Configuration.Slots))...)
	data = append(data, []byte(fmt.Sprintf("%-124s", c.Configuration.URL))...)
	data = append(data, []byte(fmt.Sprintf("%-40s", c.Configuration.SoftwareID))...)
	data = append(data, []byte(fmt.Sprintf("%-40s", c.Configuration.PackageID))...)
	log.Printf("homebrew: sending %s\n", string(data))
	_, err := c.conn.Write(data)
	return err
}

func (c *Client) sendPing() error {
	var (
		data = make([]byte, len(dmrconst.CommandRPTPING)+len(c.hexid))
		n    = copy(data, dmrconst.CommandRPTPING)
	)
	copy(data[n:], c.hexid[:])
	_, err := c.conn.Write(data)
	c.pingSent = time.Now()
	return err
}
