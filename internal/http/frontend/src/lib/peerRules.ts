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

/** The ID of a call that a peer rule checks: the destination or the caller. */
export type RuleMatch = 'destination' | 'source';

/** A routing rule of an OpenBridge peer, as the API returns it. */
export type PeerRule = {
  id: number;
  direction: boolean;
  match: RuleMatch;
  subject_id_min: number;
  subject_id_max: number;
};

/** MAX_DMR_ID is the highest DMR ID, 24 bits. */
export const MAX_DMR_ID = 16777215;

/** Returns the ranges of rules sorted and with touching or overlapping ranges joined. */
export function mergedRanges(rules: PeerRule[]): [number, number][] {
  const rs = rules
    .map((r): [number, number] => [r.subject_id_min, Math.min(r.subject_id_max, MAX_DMR_ID)])
    .filter((r) => r[0] <= r[1])
    .sort((a, b) => a[0] - b[0]);
  const out: [number, number][] = [];
  for (const r of rs) {
    const last = out[out.length - 1];
    if (last && r[0] <= last[1] + 1) last[1] = Math.max(last[1], r[1]);
    else out.push([r[0], r[1]]);
  }
  return out;
}

/** A part of a line of text; ID parts show in the monospace font. */
export type TextPart = { text: string; id?: boolean };

/** Returns the parts of the text of an ID range: one ID, "a to b", "a and up", or "any ID". */
export function rangeParts(min: number, max: number): TextPart[] {
  if (min <= 1 && max >= MAX_DMR_ID) return [{ text: 'any ID' }];
  if (max >= MAX_DMR_ID) return [{ text: String(min), id: true }, { text: ' and up' }];
  if (min === max) return [{ text: String(min), id: true }];
  return [{ text: String(min), id: true }, { text: ' to ' }, { text: String(max), id: true }];
}

/** Joins lists of parts as "a", "a and b" or "a, b and c". */
export function joinParts(items: TextPart[][]): TextPart[] {
  const out: TextPart[] = [];
  items.forEach((item, i) => {
    if (i > 0) out.push({ text: i === items.length - 1 ? ' and ' : ', ' });
    out.push(...item);
  });
  return out;
}

/** Reports whether the private call switch of a direction changes anything: not when it has source rules and no destination rules. */
export function privateApplies(rules: PeerRule[]): boolean {
  return !rules.some((r) => r.match === 'source') || rules.some((r) => r.match === 'destination');
}

/**
 * Returns the summary of the rules of one direction, as DMRHub applies them: a call passes when, for each match that has
 * rules, one of them covers it; with the private call switch, a private call needs only the source rules.
 */
export function summaryParts(verb: string, rules: PeerRule[], privateCalls: boolean): TextPart[] {
  const ids = (m: RuleMatch) => mergedRanges(rules.filter((r) => r.match === m)).map(([a, b]) => rangeParts(a, b));
  const to = ids('destination');
  const from = ids('source');
  const priv = privateCalls && privateApplies(rules);
  if (!to.length && !from.length) return [{ text: priv ? `${verb} private calls.` : `${verb} nothing.` }];
  const parts: TextPart[] = [{ text: `${verb} calls` }];
  if (to.length) {
    parts.push({ text: ' to ' }, ...joinParts(to));
    if (priv) parts.push({ text: from.length ? ', and private calls,' : ', and private calls' });
  }
  if (from.length) parts.push({ text: ' from radios ' }, ...joinParts(from));
  parts.push({ text: '.' });
  return parts;
}

/** Returns the label of a rule row: "To", "From radio" or "From radios". */
export function ruleLabel(rule: PeerRule): string {
  if (rule.match === 'destination') return 'To';
  return rule.subject_id_min === rule.subject_id_max ? 'From radio' : 'From radios';
}

/** Parses a whole decimal ID, or returns null. */
export function parseID(s: string): number | null {
  const t = s.trim();
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  return Number.isSafeInteger(n) ? n : null;
}
