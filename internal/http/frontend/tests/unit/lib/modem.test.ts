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


import { describe, expect, it } from 'vitest';
import { modemPill, modemStateOf, otherPort, portLabel, portsOf, replicaOfPort, testView, type ProbeResult } from '@/lib/modem';

const ok: ProbeResult = {
  port: '/dev/ttyAMA0', baud: 115200, opened: true, tries: 1, answered: true,
  firmware: 'MMDVM_HS_Hat-v1.6.1 20231012 14.7456MHz ADF7021', protocol: 1,
  modes: ['DMR', 'D-Star', 'YSF', 'P25', 'NXDN', 'POCSAG'],
  frequencies: { taken: true }, settings: { taken: true },
};

describe('modem ports', () => {
  it('reads the ports of each replica, only those with a path', () => {
    expect(portsOf({ replicas: [{ replica: 'a41c', self: true, ports: [{ path: '/dev/ttyACM0' }, { device: 'x' }, 3] }, { ports: [] }, { replica: 'b7', ports: null }] }))
      .toEqual([{ replica: 'a41c', self: true, ports: [{ path: '/dev/ttyACM0' }] }, { replica: 'b7', self: false, ports: [] }]);
    expect(portsOf({ data: 1 })).toEqual([]);
    expect(portsOf(null)).toEqual([]);
  });

  it('picks this replica first for a port that several replicas have', () => {
    const groups = [
      { replica: 'b7', self: false, ports: [{ path: '/dev/ttyAMA0' }, { path: '/dev/ttyACM0' }] },
      { replica: 'a41c', self: true, ports: [{ path: '/dev/ttyAMA0' }] },
    ];
    expect(replicaOfPort(groups, '/dev/ttyAMA0')).toBe('a41c');
    expect(replicaOfPort(groups, '/dev/ttyACM0')).toBe('b7');
    expect(replicaOfPort(groups, '/dev/ttyS3')).toBe('');
  });

  it('labels a link with its device', () => {
    expect(portLabel({ path: '/dev/serial/by-id/usb-MMDVM-if00', device: '/dev/ttyACM0' })).toBe('/dev/serial/by-id/usb-MMDVM-if00 (/dev/ttyACM0)');
    expect(portLabel({ path: '/dev/ttyAMA0' })).toBe('/dev/ttyAMA0');
  });

  it('types a path when the list is empty or does not have it', () => {
    const ports = [{ replica: 'a41c', self: true, ports: [{ path: '/dev/ttyAMA0' }] }];
    expect(otherPort([], '')).toBe(true);
    expect(otherPort(ports, '')).toBe(false);
    expect(otherPort(ports, '/dev/ttyAMA0')).toBe(false);
    expect(otherPort(ports, '/dev/ttyS3')).toBe(true);
  });
});

describe('modem pill', () => {
  it('shows the live state', () => {
    expect(modemPill(null)).toEqual({ tone: 'idle', text: 'Modem not running' });
    expect(modemPill(modemStateOf({ status: null, self: false }))).toEqual({ tone: 'idle', text: 'Modem not running' });
    expect(modemPill({ status: { port: '/dev/ttyAMA0', baud: 115200, linked: true }, self: true }).text).toBe('Modem connected');
    expect(modemPill({ status: { replica: '7f3c00000000000b', port: '/dev/ttyAMA0', baud: 115200, linked: true }, self: false }).text)
      .toBe('Modem connected on replica 7f3c');
    expect(modemPill({ status: { port: '/dev/ttyAMA0', baud: 115200, linked: false, error: 'no version reply from the modem' }, self: true }))
      .toEqual({ tone: 'bad', text: 'Modem not connected', title: 'no version reply from the modem' });
  });

  it('reads no state from another answer', () => {
    expect(modemStateOf({ servers: [] })).toBeNull();
    expect(modemStateOf('x')).toBeNull();
  });
});

describe('modem test result', () => {
  it('shows what the board answered', () => {
    expect(testView({ live: false, result: ok })).toEqual({
      tone: 'ok', title: 'The board answered', rows: [
        ['Firmware', 'MMDVM_HS_Hat-v1.6.1 20231012 14.7456MHz ADF7021'], ['Protocol', '1'],
        ['Modes', 'DMR, D-Star, YSF, P25, NXDN, POCSAG'], ['Frequencies', 'Taken'], ['Settings', 'Taken'],
      ],
    });
  });

  it('says what was tried and what to check when the board does not answer', () => {
    const v = testView({ live: false, result: { port: '/dev/ttyAMA0', baud: 115200, opened: true, tries: 6, answered: false, error: 'no version reply from the modem' } });
    expect(v.tone).toBe('bad');
    expect(v.title).toBe('No answer from the board');
    expect(v.text).toBe('DMRHub opened /dev/ttyAMA0 at 115200 but the board did not answer after 6 tries. Check the port and its speed, and that no other program, such as MMDVMHost, uses the port.');
  });

  it('shows why the port did not open', () => {
    const v = testView({ live: false, result: { port: '/dev/ttyACM9', baud: 115200, opened: false, tries: 0, answered: false, error: 'open /dev/ttyACM9: no such file or directory' } });
    expect(v.title).toBe('The port did not open');
    expect(v.text).toBe('DMRHub could not open /dev/ttyACM9: open /dev/ttyACM9: no such file or directory.');
  });

  it('shows a refused config', () => {
    const v = testView({ live: false, result: { ...ok, settings: { taken: false, error: 'SET_CONFIG: modem refused the command: reason 4' }, error: 'SET_CONFIG: modem refused the command: reason 4' } });
    expect(v.title).toBe('The board did not take the settings');
    expect(v.rows.at(-1)).toEqual(['Settings', 'Refused: SET_CONFIG: modem refused the command: reason 4']);
  });

  it('shows a board that DMRHub cannot drive', () => {
    const v = testView({ live: false, result: { ...ok, modes: ['D-Star'], frequencies: undefined, settings: undefined, error: 'modem firmware has no DMR' } });
    expect(v.title).toBe('DMRHub cannot drive this board');
    expect(v.rows).toEqual([['Firmware', ok.firmware], ['Protocol', '1'], ['Modes', 'D-Star']]);
  });

  it('names another replica that ran the test or did not answer', () => {
    const v = testView({ replica: '7f3c00000000000b', self: false, live: false, result: ok });
    expect(v.rows[0]).toEqual(['Replica', '7f3c']);
    const gone = testView({ replica: '7f3c00000000000b', self: false, unanswered: true, live: false });
    expect(gone.tone).toBe('bad');
    expect(gone.title).toBe('Replica 7f3c did not answer');
  });

  it('shows the live state of the running modem instead', () => {
    const live = testView({ live: true, state: { status: { port: '/dev/ttyAMA0', baud: 115200, linked: true, firmware: 'MMDVM 20260101', protocol: 2, modes: ['DMR'] }, self: true } });
    expect(live).toEqual({
      tone: 'ok', title: 'The modem is connected', rows: [['Firmware', 'MMDVM 20260101'], ['Protocol', '2'], ['Modes', 'DMR']],
      text: 'The modem runs on this port. DMRHub did not open the port again.',
    });
    const down = testView({ live: true, state: { status: { replica: '7f3c00000000000b', port: '/dev/ttyAMA0', baud: 115200, linked: false, error: 'no version reply from the modem' }, self: false } });
    expect(down.title).toBe('The modem is not connected');
    expect(down.text).toContain('on replica 7f3c');
    expect(down.text).toContain('no version reply from the modem');
  });
});
