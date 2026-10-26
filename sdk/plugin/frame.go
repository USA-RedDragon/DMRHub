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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// MaxPluginMessage is the largest message that a plugin inside DMRHub
	// sends.
	MaxPluginMessage = 64 << 10
	// MaxHubMessage is the largest message that the hub sends to a plugin
	// inside DMRHub.
	MaxHubMessage = 4 << 20
	// MaxNetChunk is the most bytes of one network read or write of a
	// plugin inside DMRHub.
	MaxNetChunk = 16 << 10
)

// ErrFrameTooLarge is returned for a message longer than its side may send.
var ErrFrameTooLarge = errors.New("the message is too large")

// WriteFrame writes m as a plugin inside DMRHub and its hub exchange
// messages on the plugin's stdin and stdout: the length of the protobuf
// encoding of m, as 4 bytes big-endian, then the encoding.
func WriteFrame(w io.Writer, m Message) error {
	b, err := marshal(m)
	if err != nil {
		return fmt.Errorf("could not encode a message: %w", err)
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b))) //nolint:gosec // messages are far shorter than 4 GiB
	copy(buf[4:], b)
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("could not write a message: %w", err)
	}
	return nil
}

// ReadFrame reads a message that WriteFrame wrote into m, and refuses one
// longer than limit.
func ReadFrame(r io.Reader, m Message, limit int) error {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return fmt.Errorf("could not read a message: %w", err)
	}
	n := binary.BigEndian.Uint32(head[:])
	if uint64(n) > uint64(limit) { //nolint:gosec // limit is positive
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return fmt.Errorf("could not read a message: %w", err)
	}
	if err := unmarshal(b, m); err != nil {
		return fmt.Errorf("could not decode a message: %w", err)
	}
	return nil
}
