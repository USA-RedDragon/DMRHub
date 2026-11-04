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
import { begin, end, routeEnded, routeStarted, show } from '@/services/busy';

describe('busy', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts the work of the page on the html element', async () => {
    const html = document.documentElement;
    begin();
    begin();
    expect(html.dataset.busy).toBe('2');
    end();
    expect(html.dataset.busy).toBe('2');
    await vi.runAllTimersAsync();
    expect(html.dataset.busy).toBe('1');
    end();
    await vi.runAllTimersAsync();
    expect(html.dataset.busy).toBe('0');
  });

  it('ends a piece of work only after the code that waits on it ran', async () => {
    const html = document.documentElement;
    const seen: string[] = [];
    begin();
    const work = Promise.resolve().then(() => {
      end();
    });
    void work.then(() => seen.push(html.dataset.busy ?? ''));
    await vi.runAllTimersAsync();
    expect(seen).toEqual(['1']);
    expect(html.dataset.busy).toBe('0');
  });

  it('counts a route change that a guard redirects once', async () => {
    const html = document.documentElement;
    routeStarted();
    routeStarted();
    expect(html.dataset.busy).toBe('1');
    routeEnded();
    await vi.runAllTimersAsync();
    expect(html.dataset.busy).toBe('0');
  });

  it('lowers the count without a document, as after a test environment goes away', async () => {
    begin();
    end();
    vi.stubGlobal('document', undefined);
    try {
      await vi.runAllTimersAsync();
    } finally {
      vi.unstubAllGlobals();
    }
    expect(document.documentElement.dataset.busy).toBe('1');
    show();
    expect(document.documentElement.dataset.busy).toBe('0');
  });
});
