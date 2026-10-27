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

/** What a repeater is, for the connect page: an MMDVM hotspot, an MMDVM repeater, or a Motorola IPSC repeater. */
export type ConnectKind = 'hotspot' | 'repeater' | 'ipsc';

/** The client programs that the connect page has steps for. */
export type ClientTab = 'pistar' | 'wpsd' | 'mmdvmhost' | 'dmrgateway' | 'droidstar' | 'hblink3' | 'other' | 'cps' | 'ipsc-other';

/** The name of each client tab, and whether its steps were tested: null for a tab that has no test mark. */
export const CLIENT_TABS: Record<ClientTab, { name: string; tested: boolean | null }> = {
  pistar: { name: 'Pi-Star', tested: true },
  wpsd: { name: 'WPSD', tested: true },
  mmdvmhost: { name: 'MMDVMHost', tested: true },
  dmrgateway: { name: 'DMRGateway', tested: true },
  droidstar: { name: 'DroidStar', tested: true },
  hblink3: { name: 'HBlink3', tested: true },
  other: { name: 'Other', tested: null },
  cps: { name: 'Motorola CPS', tested: false },
  'ipsc-other': { name: 'Other', tested: null },
};

/** Returns the kind of the repeater of type type and ID id, or null for a type that does not connect over the network. */
export function connectKind(type: string, id: number): ConnectKind | null {
  if (type === 'ipsc') return 'ipsc';
  if (type !== '' && type !== 'mmdvm') return null;
  return String(id).length === 6 ? 'repeater' : 'hotspot';
}

/** Returns the client tabs of kind, in the order of the page. */
export function clientTabs(kind: ConnectKind): ClientTab[] {
  if (kind === 'ipsc') return ['cps', 'ipsc-other'];
  return ['pistar', 'wpsd', 'mmdvmhost', 'dmrgateway', 'droidstar', 'hblink3', 'other'];
}

/** Returns the name of the device of kind in the text of the page. */
export function deviceNoun(kind: ConnectKind): string {
  return kind === 'hotspot' ? 'hotspot' : 'repeater';
}

/** Returns the name of the secret of kind: the auth key of IPSC, else the password. */
export function secretName(kind: ConnectKind): string {
  return kind === 'ipsc' ? 'auth key' : 'password';
}

/** Returns the stand-in for the secret of kind when the page does not show it. */
export function secretPlaceholder(kind: ConnectKind): string {
  if (kind === 'ipsc') return '<auth key>';
  return `<${deviceNoun(kind)} password>`;
}

/** Returns the ESSID of a 9-digit hotspot ID, its last two digits, and None for any other ID. */
export function essid(id: number): string {
  const s = String(id);
  return s.length === 9 ? s.slice(7) : 'None';
}

/** The values that the client steps fill in. */
export type ConnectValues = {
  kind: ConnectKind;
  host: string;
  port: number;
  id: number;
  password: string;
  callsign: string;
  ownerId: number | string;
  essid: string;
};

const secrets = new Map<number, string>();

/** Keeps the secret of the repeater id for the next takeSecret, as the enroll page hands it to the connect page. */
export function keepSecret(id: number, secret: string): void {
  secrets.set(id, secret);
}

/** Returns the secret that keepSecret kept for the repeater id once, and forgets it. */
export function takeSecret(id: number): string | undefined {
  const s = secrets.get(id);
  secrets.delete(id);
  return s;
}

/** A piece of a snippet; hl marks a value that DMRHub filled in. */
export type Segment = { text: string; hl?: boolean };

/** A value of a snippet that is not marked. */
export type Plain = { plain: string | number };

/** Marks a value of a snippet as text that DMRHub did not fill in. */
export function plain(v: string | number): Plain {
  return { plain: v };
}

/** Returns the segments of a template literal, with each value marked unless it is plain. */
export function snippet(strings: TemplateStringsArray, ...values: (string | number | Plain)[]): Segment[] {
  const out: Segment[] = [];
  strings.forEach((s, i) => {
    if (s) out.push({ text: s });
    if (i >= values.length) return;
    const v = values[i]!;
    if (typeof v === 'object') out.push({ text: String(v.plain) });
    else out.push({ text: String(v), hl: true });
  });
  return out;
}

/** Returns the text of segments. */
export function snippetText(segments: Segment[]): string {
  return segments.map((s) => s.text).join('');
}

/** A cell of a table of steps: text, or a value that DMRHub filled in. */
export type Cell = string | { v: string | number };

/** Marks a cell of a table of steps as a value that DMRHub filled in. */
export function val(v: string | number): Cell {
  return { v };
}
