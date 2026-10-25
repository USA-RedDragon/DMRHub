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

/** The characters of a temporary password, without look-alikes such as 0, O, 1, l and I. */
export const TEMPORARY_PASSWORD_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789';

/** The length of a temporary password. */
export const TEMPORARY_PASSWORD_LENGTH = 16;

const unbiasedLimit = 256 - (256 % TEMPORARY_PASSWORD_ALPHABET.length);

/**
 * Returns a temporary password of TEMPORARY_PASSWORD_LENGTH characters of
 * TEMPORARY_PASSWORD_ALPHABET from crypto.getRandomValues. Bytes that would
 * favor some characters are drawn again.
 */
export function temporaryPassword(): string {
  let out = '';
  const bytes = new Uint8Array(TEMPORARY_PASSWORD_LENGTH * 2);
  while (out.length < TEMPORARY_PASSWORD_LENGTH) {
    crypto.getRandomValues(bytes);
    for (const b of bytes) {
      if (b < unbiasedLimit && out.length < TEMPORARY_PASSWORD_LENGTH) {
        out += TEMPORARY_PASSWORD_ALPHABET[b % TEMPORARY_PASSWORD_ALPHABET.length];
      }
    }
  }
  return out;
}
