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

import { replicaName } from '@/lib/setupState';

/** A serial port of the replica that answers, from /config/modem/ports. */
export type SerialPort = { path: string; device?: string };

/** The live state of the modem of a replica. */
export type ModemStatus = {
  replica?: string;
  port: string;
  baud: number;
  linked: boolean;
  firmware?: string;
  protocol?: number;
  modes?: string[];
  since?: string;
  error?: string;
};

/** The answer of /config/modem. */
export type ModemState = { status: ModemStatus | null; self: boolean };

export type ProbeStep = { taken: boolean; error?: string };

/** What a test of the modem found. */
export type ProbeResult = {
  port: string;
  baud: number;
  opened: boolean;
  tries: number;
  answered: boolean;
  firmware?: string;
  protocol?: number;
  modes?: string[];
  frequencies?: ProbeStep;
  settings?: ProbeStep;
  error?: string;
};

/** The answer of /config/modem/test. */
export type ModemTest = { live: boolean; state?: ModemState; result?: ProbeResult };

export type Tone = 'ok' | 'bad' | 'idle';

const asRecord = (v: unknown): Record<string, unknown> | undefined =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined;

/** The ports of an answer of /config/modem/ports; none for anything else. */
export const portsOf = (data: unknown): SerialPort[] => {
  const ports = asRecord(data)?.ports;
  if (!Array.isArray(ports)) {
    return [];
  }
  return ports.filter((p): p is SerialPort => typeof asRecord(p)?.path === 'string');
};

/** The state of an answer of /config/modem; null for anything else. */
export const modemStateOf = (data: unknown): ModemState | null => {
  const d = asRecord(data);
  if (!d || !('status' in d)) {
    return null;
  }
  const st = asRecord(d.status);
  return { status: st && typeof st.port === 'string' ? (st as ModemStatus) : null, self: d.self === true };
};

/** The label of a port in the list: the device follows a link. */
export const portLabel = (p: SerialPort): string => (p.device ? `${p.path} (${p.device})` : p.path);

/** Whether the port select shows the box for a path that is not listed. */
export const otherPort = (ports: SerialPort[], value: string): boolean =>
  ports.length === 0 || (value !== '' && !ports.some((p) => p.path === value));

const on = (s: ModemState) => (s.self ? '' : ` on replica ${replicaName(s.status?.replica ?? '')}`);

export type Pill = { tone: Tone; text: string; title?: string };

/** The status pill of the Modem card. */
export const modemPill = (s: ModemState | null): Pill => {
  if (!s?.status) {
    return { tone: 'idle', text: 'Modem not running' };
  }
  if (s.status.linked) {
    return { tone: 'ok', text: `Modem connected${on(s)}` };
  }
  return { tone: 'bad', text: `Modem not connected${on(s)}`, title: s.status.error };
};

export type ResultView = { tone: Tone; title: string; rows: [string, string][]; text?: string };

const stepText = (s: ProbeStep | undefined): string | undefined => {
  if (!s) {
    return undefined;
  }
  return s.taken ? 'Taken' : `Refused: ${s.error ?? 'no answer'}`;
};

const boardRows = (b: { firmware?: string; protocol?: number; modes?: string[] }): [string, string][] => {
  const rows: [string, string][] = [];
  if (b.firmware) {
    rows.push(['Firmware', b.firmware]);
  }
  if (b.protocol) {
    rows.push(['Protocol', String(b.protocol)]);
  }
  if (b.modes && b.modes.length > 0) {
    rows.push(['Modes', b.modes.join(', ')]);
  }
  return rows;
};

const checkPort = 'Check the port and its speed, and that no other program, such as MMDVMHost, uses the port.';

/** How the Modem tab shows the answer of a test. */
export const testView = (t: ModemTest): ResultView => {
  if (t.live && t.state?.status) {
    const st = t.state.status;
    const where = t.state.self ? 'The modem runs on this port.' : `The modem runs on this port on replica ${replicaName(st.replica ?? '')}.`;
    if (st.linked) {
      return { tone: 'ok', title: 'The modem is connected', rows: boardRows(st), text: `${where} DMRHub did not open the port again.` };
    }
    return {
      tone: 'bad', title: 'The modem is not connected', rows: [],
      text: `${where} DMRHub keeps trying to open it${st.error ? `: ${st.error}` : ''}. ${checkPort}`,
    };
  }
  const r = t.result;
  if (!r) {
    return { tone: 'bad', title: 'The test did not run', rows: [] };
  }
  if (!r.opened) {
    return { tone: 'bad', title: 'The port did not open', rows: [], text: `DMRHub could not open ${r.port}: ${r.error ?? 'unknown error'}.` };
  }
  if (!r.answered) {
    return {
      tone: 'bad', title: 'No answer from the board', rows: [],
      text: `DMRHub opened ${r.port} at ${r.baud} but the board did not answer after ${r.tries} ${r.tries === 1 ? 'try' : 'tries'}. ${checkPort}`,
    };
  }
  const rows = boardRows(r);
  for (const [label, s] of [['Frequencies', r.frequencies], ['Settings', r.settings]] as const) {
    const text = stepText(s);
    if (text) {
      rows.push([label, text]);
    }
  }
  if (!r.error) {
    return { tone: 'ok', title: 'The board answered', rows };
  }
  if (!r.frequencies) {
    return { tone: 'bad', title: 'DMRHub cannot drive this board', rows, text: `${r.error}.` };
  }
  return { tone: 'bad', title: 'The board did not take the settings', rows };
};
