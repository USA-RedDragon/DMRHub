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

import type { BadgeTone } from '@/lib/badge';

/** Codec is the voice codec of a call or a timeslot, or '' when none was heard. */
export type Codec = '' | 'ambe' | 'codec2';

/** Codec2Mode is the Codec2 bit rate, or 0 when it is not known. */
export type Codec2Mode = 0 | 2400 | 3200;

/** SlotCodec is the codec setting of a repeater timeslot. */
export type SlotCodec = 'auto' | 'codec2' | 'ambe';

/** CallCodecFields are the codec fields of a last heard call. */
export type CallCodecFields = {
  codec?: Codec;
  codec2_mode?: Codec2Mode;
  sent_ambe?: number;
  sent_codec2?: number;
  missed_ms?: number;
  busy?: boolean;
};

/** AmbeServer is one AMBE server of the transcoding config; calls 0 means no limit. */
export type AmbeServer = {
  address: string;
  calls: number;
};

/** AmbeServerRow is an AMBE server row while it is edited; calls may hold the text typed in. */
export type AmbeServerRow = {
  address: string;
  calls: number | string;
};

/** ServerStatus is the status of one AMBE server. */
export type ServerStatus = {
  server: string;
  calls_limit: number;
  answered_seconds: number | null;
  calls: number;
};

/** TranscodingStatus is the answer of GET /api/v1/config/transcoding. */
export type TranscodingStatus = {
  servers: ServerStatus[];
};

/** CallCodec is how the codec cell of a last heard call shows. */
export type CallCodec = {
  label: string;
  tone: BadgeTone;
  lines: string[];
};

/** StatusText is how the AMBE server status line shows. */
export type StatusText = {
  tone: 'ok' | 'bad' | 'none';
  text: string;
};

const HOST_PORT = /^([^\s:[\]]+):(\d{1,5})$/;
const IPV6_PORT = /^\[([^\s[\]]*:[^\s[\]]*)\]:(\d{1,5})$/;

/** HOST_PORT_ERROR is the error shown for an AMBE server that is not a host and port. */
export const HOST_PORT_ERROR = 'Enter a host and port for each server, such as ambeserver:2460.';

/** CALLS_ERROR is the error shown for a Calls at once value that is not a whole number of 1 or more. */
export const CALLS_ERROR = 'Calls at once is a whole number of 1 or more, or empty for no limit.';

const WHOLE = /^[1-9]\d*$/;

function plural(n: number, one: string): string {
  return `${n} ${one}${n === 1 ? '' : 's'}`;
}

/** codecLabel names a codec and its Codec2 mode, as AMBE or Codec2 3200. */
export function codecLabel(codec: Codec | undefined, mode: number | undefined): string {
  if (codec === 'ambe') {
    return 'AMBE';
  }
  if (codec === 'codec2') {
    return mode === 2400 || mode === 3200 ? `Codec2 ${mode}` : 'Codec2';
  }
  return '';
}

/** callCodec returns the codec badge and lines of a voice call, or null when the call has no codec. */
export function callCodec(call: CallCodecFields): CallCodec | null {
  const label = codecLabel(call.codec, call.codec2_mode);
  if (!label) {
    return null;
  }
  if (call.busy) {
    return { label, tone: 'warn', lines: ['Not converted: every AMBE server was busy'] };
  }
  const missed = call.missed_ms ?? 0;
  if (missed > 0) {
    return {
      label,
      tone: 'warn',
      lines: [`${(missed / 1000).toFixed(1)} s not converted: AMBE server did not answer`],
    };
  }
  const lines: string[] = [];
  const ambe = call.sent_ambe ?? 0;
  const codec2 = call.sent_codec2 ?? 0;
  if (ambe > 0) {
    lines.push(`Sent as AMBE to ${plural(ambe, 'target')}`);
  }
  if (codec2 > 0) {
    lines.push(`Sent as Codec2 to ${plural(codec2, 'target')}`);
  }
  return { label, tone: call.codec === 'codec2' ? 'info' : 'none', lines };
}

/** shortTime formats a time as HH:MM in 24 h with the time zone abbreviation, as 14:02 CDT. */
export function shortTime(date: Date, timeZone?: string): string {
  const parts = new Intl.DateTimeFormat('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZoneName: 'short',
    timeZone,
  }).formatToParts(date);
  const part = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
  const hour = part('hour') === '24' ? '00' : part('hour');
  return `${hour}:${part('minute')} ${part('timeZoneName')}`;
}

/** heardLine words the codec last heard on a timeslot, or returns '' when none was heard. */
export function heardLine(
  codec: Codec | undefined,
  mode: number | undefined,
  at: string | null | undefined,
  timeZone?: string,
): string {
  const label = codecLabel(codec, mode);
  if (!label || !at) {
    return '';
  }
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `Heard: ${label}, on the last call at ${shortTime(date, timeZone)}.`;
}

/** ambeServerError returns the error of an AMBE server address, or '' when it is a host and port. */
export function ambeServerError(value: string): string {
  const v = value.trim();
  const m = HOST_PORT.exec(v) ?? IPV6_PORT.exec(v);
  if (!m) {
    return HOST_PORT_ERROR;
  }
  const port = Number(m[2]);
  if (port < 1 || port > 65535) {
    return HOST_PORT_ERROR;
  }
  return '';
}

/** callsText returns the Calls at once text of a row, which is empty for no limit. */
export function callsText(calls: number | string | undefined | null): string {
  if (calls === undefined || calls === null || calls === 0) {
    return '';
  }
  return String(calls).trim();
}

/** callsError returns the error of a Calls at once value, or '' when it is empty or a whole number of 1 or more. */
export function callsError(calls: number | string | undefined | null): string {
  const v = callsText(calls);
  if (v === '') {
    return '';
  }
  return WHOLE.test(v) && Number.isSafeInteger(Number(v)) ? '' : CALLS_ERROR;
}

/** ambeServersError returns the first error of the AMBE server rows, or '' when every row is good. */
export function ambeServersError(rows: AmbeServerRow[]): string {
  for (const row of rows) {
    const error = ambeServerError(String(row.address ?? '')) || callsError(row.calls);
    if (error) {
      return error;
    }
  }
  return '';
}

/** ambeServersPayload turns AMBE server rows into the config to save; an empty Calls at once saves as 0. */
export function ambeServersPayload(rows: AmbeServerRow[]): AmbeServer[] {
  return rows.map((row) => {
    const calls = callsText(row.calls);
    return { address: String(row.address ?? '').trim(), calls: calls === '' ? 0 : Number(calls) };
  });
}

/** serverStatus finds the status of an AMBE server address, or returns undefined when it has none. */
export function serverStatus(
  status: TranscodingStatus | null | undefined,
  address: string,
): ServerStatus | undefined {
  const a = address.trim();
  if (!a || !Array.isArray(status?.servers)) {
    return undefined;
  }
  return status.servers.find((s) => s.server === a);
}

/** statusText words the status of one AMBE server; a server without a status is checked after the save. */
export function statusText(status: ServerStatus | null | undefined): StatusText {
  if (!status) {
    return { tone: 'none', text: 'Checked after you save.' };
  }
  if (typeof status.answered_seconds === 'number') {
    const s = Math.max(0, Math.round(status.answered_seconds));
    const calls = Math.max(0, status.calls ?? 0);
    return { tone: 'ok', text: `Answered ${plural(s, 'second')} ago. ${plural(calls, 'call')} now.` };
  }
  return { tone: 'bad', text: 'The AMBE server did not answer.' };
}
