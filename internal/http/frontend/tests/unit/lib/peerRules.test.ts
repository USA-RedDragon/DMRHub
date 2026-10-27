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

import { describe, it, expect } from 'vitest';
import { mergedRanges, privateApplies, rangeParts, ruleLabel, summaryParts, parseID, type PeerRule } from '@/lib/peerRules';

const rangeText = (a: number, b: number) =>
  rangeParts(a, b)
    .map((p) => p.text)
    .join('');

let next = 1;
const rule = (match: PeerRule['match'], min: number, max = min): PeerRule => ({
  id: next++,
  direction: false,
  match,
  subject_id_min: min,
  subject_id_max: max,
});
const text = (verb: string, rules: PeerRule[], priv: boolean) =>
  summaryParts(verb, rules, priv)
    .map((p) => p.text)
    .join('');

describe('peerRules', () => {
  it('names ID ranges', () => {
    expect(rangeText(3100, 3100)).toBe('3100');
    expect(rangeText(3000001, 3000099)).toBe('3000001 to 3000099');
    expect(rangeText(5000, 16777215)).toBe('5000 and up');
    expect(rangeText(1, 16777215)).toBe('any ID');
  });

  it('joins touching ranges', () => {
    expect(mergedRanges([rule('destination', 10, 20), rule('destination', 1, 5), rule('destination', 21, 30)])).toEqual([
      [1, 5],
      [10, 30],
    ]);
  });

  it('sums up the rules as the hub applies them', () => {
    const to = [rule('destination', 3100), rule('destination', 31665)];
    const from = [rule('source', 3000001, 3000099), rule('source', 3000150)];
    expect(text('Transmits', [], false)).toBe('Transmits nothing.');
    expect(text('Transmits', [], true)).toBe('Transmits private calls.');
    expect(text('Transmits', to, false)).toBe('Transmits calls to 3100 and 31665.');
    expect(text('Transmits', to, true)).toBe('Transmits calls to 3100 and 31665, and private calls.');
    expect(text('Transmits', from, false)).toBe('Transmits calls from radios 3000001 to 3000099 and 3000150.');
    expect(text('Transmits', from, true)).toBe('Transmits calls from radios 3000001 to 3000099 and 3000150.');
    expect(text('Transmits', [...to, ...from], true)).toBe(
      'Transmits calls to 3100 and 31665, and private calls, from radios 3000001 to 3000099 and 3000150.',
    );
    expect(summaryParts('Receives', [...to, ...from], false).filter((p) => p.id).map((p) => p.text)).toEqual([
      '3100',
      '31665',
      '3000001',
      '3000099',
      '3000150',
    ]);
  });

  it('offers the private call switch only where it changes something', () => {
    expect(privateApplies([])).toBe(true);
    expect(privateApplies([rule('destination', 3100)])).toBe(true);
    expect(privateApplies([rule('source', 3000001)])).toBe(false);
    expect(privateApplies([rule('source', 3000001), rule('destination', 3100)])).toBe(true);
  });

  it('labels rules and parses IDs', () => {
    expect(ruleLabel(rule('destination', 3100))).toBe('To');
    expect(ruleLabel(rule('source', 3000150))).toBe('From radio');
    expect(ruleLabel(rule('source', 1, 2))).toBe('From radios');
    expect(parseID(' 3100 ')).toBe(3100);
    expect(parseID('31a')).toBeNull();
    expect(parseID('')).toBeNull();
  });
});
