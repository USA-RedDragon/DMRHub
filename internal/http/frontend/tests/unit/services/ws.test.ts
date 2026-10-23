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

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import ws from '@/services/ws';

class FakeSocket {
  static sockets: FakeSocket[] = [];
  url: string;
  listeners: Record<string, ((event: unknown) => void)[]> = {};
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    FakeSocket.sockets.push(this);
  }

  addEventListener(type: string, fn: (event: unknown) => void) {
    (this.listeners[type] ??= []).push(fn);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close() {}

  emit(type: string, event: unknown = {}) {
    for (const fn of this.listeners[type] ?? []) {
      fn(event);
    }
  }
}

describe('ws service', () => {
  beforeEach(() => {
    FakeSocket.sockets = [];
    vi.useFakeTimers();
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('reconnects every time the server closes with 1008', () => {
    const conn = ws.connect('ws://hub/ws/calls', () => {});
    for (let i = 1; i <= 8; i++) {
      const socket = FakeSocket.sockets[i - 1]!;
      socket.emit('open');
      socket.emit('close', { code: 1008, reason: 'user changed' });
      vi.advanceTimersByTime(20000);
      expect(FakeSocket.sockets).toHaveLength(i + 1);
    }
    expect(FakeSocket.sockets.at(-1)!.url).toBe('ws://hub/ws/calls');
    conn.close();
  });

  it('stops reconnecting after close is called', () => {
    const conn = ws.connect('ws://hub/ws/calls', () => {});
    conn.close();
    FakeSocket.sockets[0]!.emit('close', { code: 1008, reason: 'logged out' });
    vi.advanceTimersByTime(20000);
    expect(FakeSocket.sockets).toHaveLength(1);
  });
});
