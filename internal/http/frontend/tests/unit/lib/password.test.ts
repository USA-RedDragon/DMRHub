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

import { describe, it, expect, vi, afterEach } from 'vitest';
import { TEMPORARY_PASSWORD_ALPHABET, temporaryPassword } from '@/lib/password';

describe('temporaryPassword', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('has 16 characters without look-alikes', () => {
    expect(TEMPORARY_PASSWORD_ALPHABET).not.toMatch(/[0O1lI]/);
    expect(new Set(TEMPORARY_PASSWORD_ALPHABET).size).toBe(TEMPORARY_PASSWORD_ALPHABET.length);
    for (let i = 0; i < 200; i++) {
      expect(temporaryPassword()).toMatch(new RegExp(`^[${TEMPORARY_PASSWORD_ALPHABET}]{16}$`));
    }
  });

  it('comes from crypto.getRandomValues', () => {
    const spy = vi.spyOn(crypto, 'getRandomValues');
    temporaryPassword();
    expect(spy).toHaveBeenCalled();
  });

  it('draws again the bytes that would favor some characters', () => {
    let call = 0;
    vi.spyOn(crypto, 'getRandomValues').mockImplementation(<T extends ArrayBufferView | null>(array: T): T => {
      const bytes = array as unknown as Uint8Array;
      bytes.fill(call === 0 ? 255 : 1);
      call++;
      return array;
    });
    expect(temporaryPassword()).toBe(TEMPORARY_PASSWORD_ALPHABET[1]!.repeat(16));
    expect(call).toBe(2);
  });

  it('gives a different password each time', () => {
    const seen = new Set(Array.from({ length: 100 }, () => temporaryPassword()));
    expect(seen.size).toBe(100);
  });
});
