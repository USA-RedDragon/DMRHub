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
import {
  CALLS_ERROR,
  HOST_PORT_ERROR,
  ambeServerError,
  ambeServersError,
  ambeServersPayload,
  callCodec,
  callsError,
  callsText,
  codecLabel,
  heardLine,
  serverStatus,
  shortTime,
  statusText,
} from '@/lib/codec';

describe('codecLabel', () => {
  it('names AMBE and each Codec2 mode', () => {
    expect(codecLabel('ambe', 0)).toBe('AMBE');
    expect(codecLabel('codec2', 2400)).toBe('Codec2 2400');
    expect(codecLabel('codec2', 3200)).toBe('Codec2 3200');
    expect(codecLabel('codec2', 0)).toBe('Codec2');
    expect(codecLabel('', 0)).toBe('');
    expect(codecLabel(undefined, undefined)).toBe('');
  });
});

describe('callCodec', () => {
  it('has no cell for a call without a codec', () => {
    expect(callCodec({})).toBeNull();
    expect(callCodec({ codec: '', sent_ambe: 3 })).toBeNull();
  });

  it('shows a Codec2 call sent as AMBE', () => {
    expect(callCodec({ codec: 'codec2', codec2_mode: 3200, sent_ambe: 4 })).toEqual({
      label: 'Codec2 3200',
      tone: 'info',
      lines: ['Sent as AMBE to 4 targets'],
    });
  });

  it('shows an AMBE call sent as Codec2 to one target', () => {
    expect(callCodec({ codec: 'ambe', sent_codec2: 1 })).toEqual({
      label: 'AMBE',
      tone: 'none',
      lines: ['Sent as Codec2 to 1 target'],
    });
  });

  it('shows no line when nothing was converted', () => {
    expect(callCodec({ codec: 'ambe', sent_ambe: 0, sent_codec2: 0, missed_ms: 0 })).toEqual({
      label: 'AMBE',
      tone: 'none',
      lines: [],
    });
  });

  it('shows both lines when a call went out in both codecs', () => {
    expect(callCodec({ codec: 'codec2', codec2_mode: 2400, sent_ambe: 1, sent_codec2: 2 })!.lines).toEqual([
      'Sent as AMBE to 1 target',
      'Sent as Codec2 to 2 targets',
    ]);
  });

  it('warns about audio that was not converted, without a sent line', () => {
    expect(callCodec({ codec: 'codec2', codec2_mode: 3200, sent_ambe: 4, missed_ms: 1234 })).toEqual({
      label: 'Codec2 3200',
      tone: 'warn',
      lines: ['1.2 s not converted: AMBE server did not answer'],
    });
    expect(callCodec({ codec: 'ambe', missed_ms: 40 })!.lines).toEqual([
      '0.0 s not converted: AMBE server did not answer',
    ]);
  });

  it('warns that a call was not converted because every AMBE server was busy', () => {
    expect(callCodec({ codec: 'codec2', codec2_mode: 3200, busy: true })).toEqual({
      label: 'Codec2 3200',
      tone: 'warn',
      lines: ['Not converted: every AMBE server was busy'],
    });
  });

  it('shows only the busy line when the call was also missed or sent', () => {
    expect(callCodec({ codec: 'codec2', codec2_mode: 3200, busy: true, missed_ms: 1234, sent_ambe: 2 })!.lines).toEqual([
      'Not converted: every AMBE server was busy',
    ]);
  });

  it('ignores busy when it is false', () => {
    expect(callCodec({ codec: 'codec2', codec2_mode: 3200, busy: false, sent_ambe: 4 })).toEqual({
      label: 'Codec2 3200',
      tone: 'info',
      lines: ['Sent as AMBE to 4 targets'],
    });
  });
});

describe('shortTime', () => {
  it('shows 24 h time with the zone abbreviation', () => {
    expect(shortTime(new Date('2026-10-23T19:02:00Z'), 'America/Chicago')).toBe('14:02 CDT');
    expect(shortTime(new Date('2026-12-23T20:02:00Z'), 'America/Chicago')).toBe('14:02 CST');
  });

  it('shows midnight as 00', () => {
    expect(shortTime(new Date('2026-10-24T05:07:00Z'), 'America/Chicago')).toBe('00:07 CDT');
  });
});

describe('heardLine', () => {
  it('words the last heard codec and time', () => {
    expect(heardLine('codec2', 3200, '2026-10-23T19:02:00Z', 'America/Chicago')).toBe(
      'Heard: Codec2 3200, on the last call at 14:02 CDT.',
    );
    expect(heardLine('ambe', 0, '2026-10-23T18:47:30Z', 'America/Chicago')).toBe(
      'Heard: AMBE, on the last call at 13:47 CDT.',
    );
    expect(heardLine('codec2', 0, '2026-10-23T18:47:30Z', 'America/Chicago')).toBe(
      'Heard: Codec2, on the last call at 13:47 CDT.',
    );
  });

  it('is empty when the timeslot was never heard', () => {
    expect(heardLine('', 0, null)).toBe('');
    expect(heardLine('ambe', 0, null)).toBe('');
    expect(heardLine('ambe', 0, 'not a time')).toBe('');
  });
});

describe('ambeServerError', () => {
  it('rejects an empty value', () => {
    expect(ambeServerError('')).toBe(HOST_PORT_ERROR);
    expect(ambeServerError('  ')).toBe(HOST_PORT_ERROR);
  });

  it('accepts a host or an address with a port', () => {
    for (const v of ['ambeserver:2460', 'ambeserver.example.org:2460', '192.0.2.10:1', '[::1]:2460', '[2001:db8::5]:65535',
      ' ambeserver:2460 ']) {
      expect(ambeServerError(v), v).toBe('');
    }
  });

  it('rejects a value that is not a host and port', () => {
    for (const v of ['ambeserver', 'ambeserver:', ':2460', 'ambe server:2460', '::1:2460', '[::1]', '[]:2460',
      'host:port', 'a:b:2460', 'host:123456']) {
      expect(ambeServerError(v), v).toBe(HOST_PORT_ERROR);
    }
  });

  it('rejects port 0 and ports over 65535', () => {
    expect(ambeServerError('ambeserver:0')).toBe(HOST_PORT_ERROR);
    expect(ambeServerError('ambeserver:65536')).toBe(HOST_PORT_ERROR);
    expect(ambeServerError('[::1]:99999')).toBe(HOST_PORT_ERROR);
  });

  it('uses the text of the mockup', () => {
    expect(HOST_PORT_ERROR).toBe('Enter a host and port for each server, such as ambeserver:2460.');
  });
});

describe('callsText', () => {
  it('shows no limit as empty', () => {
    expect(callsText(0)).toBe('');
    expect(callsText('')).toBe('');
    expect(callsText(undefined)).toBe('');
    expect(callsText(null)).toBe('');
  });

  it('shows a limit as its number', () => {
    expect(callsText(1)).toBe('1');
    expect(callsText(12)).toBe('12');
    expect(callsText(' 3 ')).toBe('3');
  });
});

describe('callsError', () => {
  it('accepts empty and whole numbers of 1 or more', () => {
    for (const v of ['', '  ', 0, 1, 4, '1', '16', ' 2 ']) {
      expect(callsError(v), String(v)).toBe('');
    }
  });

  it('rejects anything else', () => {
    for (const v of ['0', '-1', -1, '1.5', 1.5, 'two', '01', '1e3', '99999999999999999999']) {
      expect(callsError(v), String(v)).toBe(CALLS_ERROR);
    }
  });

  it('uses the text of the mockup', () => {
    expect(CALLS_ERROR).toBe('Calls at once is a whole number of 1 or more, or empty for no limit.');
  });
});

describe('ambeServersError', () => {
  it('accepts no rows and good rows', () => {
    expect(ambeServersError([])).toBe('');
    expect(ambeServersError([{ address: 'ambeserver:2460', calls: 0 }, { address: '[::1]:2460', calls: '1' }])).toBe('');
  });

  it('returns the first error', () => {
    expect(ambeServersError([{ address: 'ambeserver:2460', calls: 'x' }, { address: '', calls: '' }])).toBe(CALLS_ERROR);
    expect(ambeServersError([{ address: 'ambeserver', calls: 'x' }])).toBe(HOST_PORT_ERROR);
    expect(ambeServersError([{ address: 'ambeserver:2460', calls: '' }, { address: '', calls: '' }])).toBe(HOST_PORT_ERROR);
  });
});

describe('ambeServersPayload', () => {
  it('saves an empty limit as 0 and a limit as a number', () => {
    expect(ambeServersPayload([
      { address: ' ambeserver:2460 ', calls: '' },
      { address: 'thumbdv.example.org:2460', calls: '1' },
      { address: '[::1]:2460', calls: 3 },
      { address: 'a:1', calls: 0 },
    ])).toEqual([
      { address: 'ambeserver:2460', calls: 0 },
      { address: 'thumbdv.example.org:2460', calls: 1 },
      { address: '[::1]:2460', calls: 3 },
      { address: 'a:1', calls: 0 },
    ]);
  });
});

describe('serverStatus', () => {
  const status = {
    servers: [
      { server: 'ambeserver:2460', calls_limit: 0, answered_seconds: 2, calls: 3 },
      { server: 'thumbdv.example.org:2460', calls_limit: 1, answered_seconds: null, calls: 0 },
    ],
  };

  it('finds the status of an address', () => {
    expect(serverStatus(status, 'thumbdv.example.org:2460')).toBe(status.servers[1]);
    expect(serverStatus(status, ' ambeserver:2460 ')).toBe(status.servers[0]);
  });

  it('has no status for an address the server does not know', () => {
    expect(serverStatus(status, 'new.example.org:2460')).toBeUndefined();
    expect(serverStatus(status, '')).toBeUndefined();
    expect(serverStatus(null, 'ambeserver:2460')).toBeUndefined();
    expect(serverStatus({} as never, 'ambeserver:2460')).toBeUndefined();
  });
});

describe('statusText', () => {
  const answered = (seconds: number | null, calls: number) => ({
    server: 'ambeserver:2460',
    calls_limit: 0,
    answered_seconds: seconds,
    calls,
  });

  it('says a server without a status is checked after the save', () => {
    expect(statusText(undefined)).toEqual({ tone: 'none', text: 'Checked after you save.' });
    expect(statusText(null)).toEqual({ tone: 'none', text: 'Checked after you save.' });
  });

  it('says when the AMBE server last answered and how many calls it has', () => {
    expect(statusText(answered(2, 3))).toEqual({ tone: 'ok', text: 'Answered 2 seconds ago. 3 calls now.' });
    expect(statusText(answered(1, 1)).text).toBe('Answered 1 second ago. 1 call now.');
    expect(statusText(answered(0, 0)).text).toBe('Answered 0 seconds ago. 0 calls now.');
    expect(statusText(answered(1.6, 2)).text).toBe('Answered 2 seconds ago. 2 calls now.');
  });

  it('says when the AMBE server did not answer', () => {
    expect(statusText(answered(null, 0))).toEqual({ tone: 'bad', text: 'The AMBE server did not answer.' });
  });
});
