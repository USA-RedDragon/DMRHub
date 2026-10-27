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

const v4 = (s: string): bigint | null => {
  const parts = s.split('.');
  if (parts.length !== 4) {
    return null;
  }
  let n = 0n;
  for (const p of parts) {
    if (!/^\d{1,3}$/.test(p) || Number(p) > 255) {
      return null;
    }
    n = (n << 8n) | BigInt(Number(p));
  }
  return n;
};

const v6 = (input: string): bigint | null => {
  let s = input;
  if (!/^[0-9a-fA-F:.]+$/.test(s)) {
    return null;
  }
  let tail: bigint | null = null;
  if (s.includes('.')) {
    const k = s.lastIndexOf(':');
    tail = v4(s.slice(k + 1));
    if (tail === null) {
      return null;
    }
    s = `${s.slice(0, k + 1)}0:0`;
  }
  const halves = s.split('::');
  if (halves.length > 2) {
    return null;
  }
  const head = halves[0] ? halves[0].split(':') : [];
  const rest = halves.length === 2 && halves[1] ? halves[1].split(':') : [];
  const missing = 8 - head.length - rest.length;
  if ((halves.length === 1 && missing !== 0) || (halves.length === 2 && missing < 1)) {
    return null;
  }
  const groups = [...head, ...Array<string>(halves.length === 2 ? missing : 0).fill('0'), ...rest];
  let n = 0n;
  for (const g of groups) {
    if (!/^[0-9a-fA-F]{1,4}$/.test(g)) {
      return null;
    }
    n = (n << 16n) | BigInt(parseInt(g, 16));
  }
  if (tail !== null) {
    n = (n & ~0xffffffffn) | tail;
  }
  return n;
};

const show4 = (n: bigint): string => [24n, 16n, 8n, 0n].map((k) => String((n >> k) & 255n)).join('.');

const show6 = (n: bigint): string => {
  const groups: string[] = [];
  for (let k = 112n; k >= 0n; k -= 16n) {
    groups.push(((n >> k) & 0xffffn).toString(16));
  }
  let best = -1;
  let bestLen = 1;
  for (let i = 0; i < 8;) {
    let j = i;
    while (j < 8 && groups[j] === '0') {
      j++;
    }
    if (j - i > bestLen) {
      best = i;
      bestLen = j - i;
    }
    i = j === i ? i + 1 : j;
  }
  if (best < 0) {
    return groups.join(':');
  }
  return `${groups.slice(0, best).join(':')}::${groups.slice(best + bestLen).join(':')}`;
};

/**
 * Why line is not an entry of an IP list, as the server checks it, or
 * undefined for an entry or an empty line. An entry is an IPv4 or IPv6
 * address or a CIDR range with no bits set after its prefix.
 */
export const ipEntryError = (line: string): string | undefined => {
  const s = line.trim();
  if (s === '') {
    return undefined;
  }
  const bad = `${s} is not an IP address or CIDR range`;
  const [addr = '', bits, more] = s.split('/');
  if (more !== undefined) {
    return bad;
  }
  let width = 32;
  let n = v4(addr);
  if (n === null) {
    width = 128;
    n = v6(addr);
  }
  if (n === null) {
    return bad;
  }
  if (bits === undefined) {
    return undefined;
  }
  if (!/^\d{1,3}$/.test(bits) || Number(bits) > width) {
    return bad;
  }
  const host = (1n << BigInt(width - Number(bits))) - 1n;
  if ((n & host) !== 0n) {
    const masked = n & ~host;
    return `${s} has bits set after its prefix; use ${width === 32 ? show4(masked) : show6(masked)}/${bits}`;
  }
  return undefined;
};

/** The error of the first entry of lines that is not valid. */
export const ipListError = (lines: unknown): string | undefined => {
  if (!Array.isArray(lines)) {
    return undefined;
  }
  for (const l of lines) {
    const e = ipEntryError(String(l));
    if (e) {
      return e;
    }
  }
  return undefined;
};

/** The number of entries of lines, without the empty lines. */
export const ipListCount = (lines: unknown): number =>
  Array.isArray(lines) ? lines.filter((l) => String(l).trim() !== '').length : 0;
