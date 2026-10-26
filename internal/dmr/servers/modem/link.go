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

package modem

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

const (
	readSize   = 1024
	frameQueue = 64
	writeQueue = 128
)

var errLinkClosed = errors.New("modem link closed")

// splitFrames emits the whole frames at the start of p, drops the octets
// that start no frame, and returns the octets of a frame that is not
// whole yet, moved to the start of p. A frame's payload points into p, so
// emit must copy what it keeps.
func splitFrames(p []byte, emit func(mmdvm.Frame)) []byte {
	off := 0
	for off < len(p) {
		f, n, res := mmdvm.Next(p[off:])
		switch res {
		case mmdvm.NextFrame:
			emit(f)
			off += n
		case mmdvm.NextSkip:
			off += max(n, 1)
		case mmdvm.NextMore:
			if len(p)-off >= mmdvm.MaxFrameLen {
				off++
				continue
			}
			return p[:copy(p, p[off:])]
		}
	}
	return p[:0]
}

// link reads frames from a modem port and writes frames to it, each on its
// own goroutine.
type link struct {
	port   Port
	frames chan mmdvm.Frame
	out    chan []byte
	done   chan struct{}
	flush  chan struct{}
	idle   chan struct{}
	failed chan struct{}

	errOnce   sync.Once
	err       error
	closeOnce sync.Once
	flushOnce sync.Once
	wg        sync.WaitGroup
}

func newLink(p Port) *link {
	l := &link{port: p, frames: make(chan mmdvm.Frame, frameQueue), out: make(chan []byte, writeQueue),
		done: make(chan struct{}), flush: make(chan struct{}), idle: make(chan struct{}), failed: make(chan struct{})}
	l.wg.Add(2)
	go l.read()
	go l.write()
	return l
}

func (l *link) fail(err error) {
	l.errOnce.Do(func() {
		l.err = err
		close(l.failed)
	})
}

// cause returns why the link failed.
func (l *link) cause() error {
	select {
	case <-l.failed:
		return l.err
	default:
		return nil
	}
}

func (l *link) read() {
	defer l.wg.Done()
	buf := make([]byte, readSize)
	pending := make([]byte, 0, mmdvm.MaxFrameLen+readSize)
	for {
		n, err := l.port.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			pending = splitFrames(pending, l.deliver)
		}
		if err != nil {
			l.fail(err)
			return
		}
	}
}

func (l *link) deliver(f mmdvm.Frame) {
	f.Payload = append([]byte(nil), f.Payload...)
	select {
	case l.frames <- f:
	case <-l.done:
	}
}

func (l *link) write() {
	defer l.wg.Done()
	defer close(l.idle)
	for {
		select {
		case b := <-l.out:
			if _, err := l.port.Write(b); err != nil {
				l.fail(err)
				return
			}
		case <-l.flush:
			for {
				select {
				case b := <-l.out:
					if _, err := l.port.Write(b); err != nil {
						l.fail(err)
						return
					}
				default:
					return
				}
			}
		case <-l.done:
			return
		}
	}
}

// send queues frame f for the modem. It drops f when the queue is full or
// the link failed.
func (l *link) send(f []byte) {
	select {
	case <-l.failed:
		return
	default:
	}
	select {
	case l.out <- f:
	default:
		slog.Warn("Modem write queue full, dropping a frame")
	}
}

// finish writes the queued frames, for at most timeout, then closes the
// link.
func (l *link) finish(timeout time.Duration) {
	l.flushOnce.Do(func() { close(l.flush) })
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-l.idle:
	case <-t.C:
	}
	l.close()
}

// close closes the port and waits for the goroutines of the link.
func (l *link) close() {
	l.closeOnce.Do(func() {
		close(l.done)
		_ = l.port.Close()
		l.fail(errLinkClosed)
	})
	l.wg.Wait()
}
