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

import { ambeServersPayload, type AmbeServerRow } from '@/lib/codec';
import { formatValue, type Setting, type SetupState } from '@/lib/setupState';

export type Values = Record<string, unknown>;

/** Reads the value of the setting at path from the values on the page. */
export type Read = (path: string) => unknown;

export type FieldKind =
  | 'text'
  | 'number'
  | 'bool'
  | 'select'
  | 'choice'
  | 'list'
  | 'textarea'
  | 'secret'
  | 'seconds'
  | 'minutes'
  | 'mhz'
  | 'mode'
  | 'port';

export type Option = { value: string | number; label: string; description?: string };

/** One setting on a tab of the Setup page. */
export type FieldDef = {
  path: string;
  label: string;
  kind: FieldKind;
  help?: string | ((read: Read) => string);
  options?: Option[];
  show?: (read: Read) => boolean;
  off?: (read: Read) => boolean;
  also?: (value: unknown, read: Read) => [string, unknown][];
  placeholder?: string;
  step?: string;
  uppercase?: boolean;
  required?: boolean;
};

export type GridRow = { grid: FieldDef[] };
export type DetailsRow = { details: string; rows: Row[] };
export type TextRow = { text: string | ((read: Read) => string) };
export type Row = FieldDef | GridRow | DetailsRow | TextRow;

export type CardDef = {
  title: string;
  rows: Row[];
  cards?: CardDef[];
  showCards?: (read: Read) => boolean;
  pill?: 'modem';
  custom?: 'modem-test';
};

export type IntroPart = string | { text: string; to: string };

export type TabGroup = 'Server' | 'DMR' | 'Features';

/** One tab of the Setup page, at /admin/setup#id. */
export type TabDef = {
  id: string;
  group: TabGroup;
  name: string;
  intro: IntroPart[];
  cards: CardDef[];
  custom?: 'transcoding';
  saveNote?: string;
  optional?: boolean;
};

export const isGrid = (r: Row): r is GridRow => 'grid' in r;
export const isDetails = (r: Row): r is DetailsRow => 'details' in r;
export const isText = (r: Row): r is TextRow => 'text' in r;
export const isField = (r: Row): r is FieldDef => 'path' in r;

const on = (path: string) => (read: Read) => read(path) === true;

const hangHelp = (kind: string) =>
  `Data and CSBK calls to a slot wait this long after a ${kind} call on it. 0 is off, at most 10 seconds.`;

const reach: Option[] = [
  {
    value: 'hybrid',
    label: 'Hybrid',
    description:
      'Any radio heard on a repeater of this network. A radio ID that a user owns counts only on the repeaters of that user or the ones that an admin trusts.',
  },
  {
    value: 'members',
    label: 'Members only',
    description: 'Only the radio IDs that users own, on the repeaters of their owner or the ones that an admin trusts.',
  },
  { value: 'anyone', label: 'Anyone', description: 'Any radio, wherever DMRHub last heard it, with no owner check.' },
];

const unknown: Option[] = [
  { value: 'drop', label: 'Drop the call and log it', description: 'DMRHub logs the call and sends it nowhere.' },
  { value: 'flood', label: 'Send it to every repeater', description: 'Every repeater and hotspot of the network gets the call.' },
];

const sameAsVoice: Option = { value: '', label: 'The same as voice calls' };

/** The tabs of the Setup page, in the order of the menu. */
export const TABS: TabDef[] = [
  {
    id: 'general',
    group: 'Server',
    name: 'General',
    intro: ['The name of the network, the log, new users and the secrets.'],
    cards: [
      {
        title: 'General',
        rows: [
          { path: 'network-name', label: 'Network name', kind: 'text', help: 'The name of the network, shown at the top of every page.' },
          {
            path: 'log-level', label: 'Log level', kind: 'select', help: 'How much DMRHub writes to its log.',
            options: ['debug', 'info', 'warn', 'error'].map((v) => ({ value: v, label: v })),
          },
          {
            path: 'auto-approve-users', label: 'Approve new users at once', kind: 'bool',
            help: 'New users can log in as soon as they register, with no wait for an admin.',
          },
        ],
      },
      {
        title: 'Secrets',
        rows: [
          { path: 'secret', label: 'Secret', kind: 'secret', help: 'Signs the sessions of the users. At least 15 random characters.' },
          { path: 'password-salt', label: 'Password salt', kind: 'secret', help: 'Salt for the password hashes of the users. Different from the secret.' },
          { path: 'hibp-api-key', label: 'Have I Been Pwned API key', kind: 'secret', help: 'Checks new passwords against known leaks. Empty turns the check off.' },
        ],
      },
    ],
  },
  {
    id: 'http',
    group: 'Server',
    name: 'HTTP',
    intro: ['The URL of the site, robots.txt and the web server.'],
    cards: [
      {
        title: 'Site',
        rows: [
          {
            path: 'http.canonical-host', label: 'Site URL', kind: 'text', placeholder: 'https://dmrhub.example.org',
            help: 'The URL that people use to reach this site. Unless the DMR and MMDVM tab sets a public server address, hotspots connect to its host name.',
          },
          {
            path: 'http.robots-txt.mode', label: 'robots.txt', kind: 'select',
            options: [
              { value: 'disabled', label: 'Ask search engines not to index the site' },
              { value: 'allow', label: 'Let search engines index the site' },
              { value: 'custom', label: 'Use my own robots.txt' },
            ],
          },
          { path: 'http.robots-txt.content', label: 'robots.txt content', kind: 'textarea', show: (read) => read('http.robots-txt.mode') === 'custom' },
        ],
      },
      {
        title: 'Web server',
        rows: [
          { path: 'http.bind', label: 'Bind address', kind: 'text', help: 'The address that the web server listens on.' },
          { path: 'http.port', label: 'Port', kind: 'number', help: 'The TCP port of the web server.' },
          { path: 'http.trusted-proxies', label: 'Trusted proxies', kind: 'list', help: 'Addresses of the reverse proxies in front of DMRHub, one for each line.' },
          { path: 'http.cors.enabled', label: 'Allow other sites to call the API (CORS)', kind: 'bool' },
          {
            path: 'http.cors.extra-hosts', label: 'Other allowed origins', kind: 'list',
            help: 'One for each line, such as https://radio.example.org.', show: on('http.cors.enabled'),
          },
        ],
      },
    ],
  },
  {
    id: 'database',
    group: 'Server',
    name: 'Database',
    intro: ['Where DMRHub keeps its data, the Setup settings among them.'],
    cards: [
      {
        title: 'Database',
        rows: [
          {
            path: 'database.driver', label: 'Driver', kind: 'select',
            help: 'SQLite for small networks and tests. PostgreSQL or MySQL for more than one replica.',
            options: [
              { value: 'sqlite', label: 'SQLite' },
              { value: 'postgres', label: 'PostgreSQL' },
              { value: 'mysql', label: 'MySQL or MariaDB' },
            ],
          },
          { path: 'database.database', label: 'Database', kind: 'text', help: 'The name of the database, or the path of the SQLite file.' },
          { path: 'database.host', label: 'Host', kind: 'text' },
          { path: 'database.port', label: 'Port', kind: 'number' },
          { path: 'database.username', label: 'Username', kind: 'text' },
          { path: 'database.password', label: 'Password', kind: 'secret' },
          { path: 'database.extra-parameters', label: 'Extra parameters', kind: 'list', help: 'Added to the connection, one for each line.' },
        ],
      },
    ],
  },
  {
    id: 'redis',
    group: 'Server',
    name: 'Redis',
    intro: ['Redis shares calls and state between replicas.'],
    cards: [
      {
        title: 'Redis',
        rows: [
          { path: 'redis.enabled', label: 'Use Redis', kind: 'bool', help: 'Redis is needed for more than one replica.' },
          { path: 'redis.host', label: 'Host', kind: 'text' },
          { path: 'redis.port', label: 'Port', kind: 'number' },
          { path: 'redis.password', label: 'Password', kind: 'secret' },
        ],
      },
    ],
  },
  {
    id: 'smtp',
    group: 'Server',
    name: 'SMTP',
    intro: ['The mail server that DMRHub sends email through.'],
    cards: [
      {
        title: 'Email',
        rows: [
          { path: 'smtp.enabled', label: 'Send email', kind: 'bool', help: 'For password resets and notices to users.' },
          { path: 'smtp.host', label: 'SMTP host', kind: 'text' },
          { path: 'smtp.port', label: 'SMTP port', kind: 'number' },
          {
            path: 'smtp.tls', label: 'TLS', kind: 'select',
            options: [
              { value: 'none', label: 'None' },
              { value: 'starttls', label: 'STARTTLS' },
              { value: 'implicit', label: 'Implicit TLS' },
            ],
          },
          {
            path: 'smtp.auth-method', label: 'Authentication', kind: 'select',
            options: [
              { value: 'none', label: 'None' },
              { value: 'plain', label: 'Plain' },
              { value: 'login', label: 'Login' },
            ],
          },
          { path: 'smtp.username', label: 'Username', kind: 'text' },
          { path: 'smtp.password', label: 'Password', kind: 'secret' },
          { path: 'smtp.from', label: 'From address', kind: 'text' },
        ],
      },
    ],
  },
  {
    id: 'metrics',
    group: 'Server',
    name: 'Metrics and PProf',
    intro: ['Monitoring and debugging listeners.'],
    cards: [
      {
        title: 'Metrics',
        rows: [
          { path: 'metrics.enabled', label: 'Serve metrics', kind: 'bool', help: 'Prometheus metrics on their own port.' },
          { path: 'metrics.bind', label: 'Bind address', kind: 'text' },
          { path: 'metrics.port', label: 'Port', kind: 'number' },
          { path: 'metrics.trusted-proxies', label: 'Trusted proxies', kind: 'list' },
          { path: 'metrics.otlp-endpoint', label: 'OTLP endpoint', kind: 'text', help: 'Where DMRHub sends its traces. Empty sends none.' },
        ],
      },
      {
        title: 'PProf',
        rows: [
          { path: 'pprof.enabled', label: 'Serve PProf', kind: 'bool', help: 'Profiles for debugging. Leave off unless a developer asks for them.' },
          { path: 'pprof.bind', label: 'Bind address', kind: 'text', show: on('pprof.enabled') },
          { path: 'pprof.port', label: 'Port', kind: 'number', show: on('pprof.enabled') },
          { path: 'pprof.trusted-proxies', label: 'Trusted proxies', kind: 'list', show: on('pprof.enabled') },
        ],
      },
    ],
  },
  {
    id: 'dmr',
    group: 'DMR',
    name: 'DMR and MMDVM',
    intro: ['Hotspots, MMDVMHost, DMRGateway, DroidStar and HBlink connect to the MMDVM server. It is always on.'],
    cards: [
      {
        title: 'MMDVM server',
        rows: [
          { path: 'dmr.mmdvm.bind', label: 'Bind address', kind: 'text', help: 'The address that the MMDVM server listens on.' },
          { path: 'dmr.mmdvm.port', label: 'Port', kind: 'number', help: 'The UDP port that hotspots connect to. The enroll page shows this port to users.' },
          {
            path: 'dmr.public-address', label: 'Public server address', kind: 'text', placeholder: 'dmr.example.org',
            help: 'The host name or IP address that hotspots and repeaters connect to. The enroll page shows it to users. Empty uses the host name of the site URL.',
          },
        ],
      },
      {
        title: 'Radio IDs',
        rows: [
          {
            path: 'dmr.disable-radio-id-validation', label: 'Turn off the RadioID.net checks', kind: 'bool',
            help: 'With the checks off, any ID of 7 to 9 digits can be used, and hotspot IDs need not start with the DMR ID of the owner.',
          },
          { path: 'dmr.radio-id-url', label: 'Radio ID list', kind: 'text', help: 'In the format of RadioID.net.' },
          { path: 'dmr.repeater-id-url', label: 'Repeater ID list', kind: 'text', help: 'In the format of RadioID.net.' },
        ],
      },
      {
        title: 'Records',
        rows: [
          { path: 'dmr.retention.transmissions', label: 'Keep last heard (days)', kind: 'number', help: '0 keeps them forever.' },
          { path: 'dmr.retention.bursts', label: 'Keep raw bursts (days)', kind: 'number', help: '0 keeps them forever.' },
        ],
      },
      {
        title: 'Replicas',
        rows: [
          {
            path: 'dmr.drain-timeout', label: 'Hand-over time (seconds)', kind: 'number',
            help: 'How long a replica that stops keeps serving while other replicas take over. Shorter than the stop time of the orchestrator. 0 stops at once.',
          },
        ],
      },
    ],
  },
  {
    id: 'ipsc',
    group: 'DMR',
    name: 'IPSC',
    intro: ['Motorola repeaters, such as the XPR 8400 and SLR 5500, connect over IPSC. Users enroll them as Motorola IPSC.'],
    cards: [
      {
        title: 'IPSC server',
        rows: [
          { path: 'dmr.ipsc.enabled', label: 'Accept Motorola repeaters (IPSC)', kind: 'bool' },
          { path: 'dmr.ipsc.bind', label: 'Bind address', kind: 'text', show: on('dmr.ipsc.enabled') },
          { path: 'dmr.ipsc.port', label: 'Port', kind: 'number', help: 'The UDP port that Motorola repeaters connect to.', show: on('dmr.ipsc.enabled') },
          {
            path: 'dmr.ipsc.network-id', label: 'Network ID', kind: 'number', required: true,
            help: 'The DMR ID that this server uses toward IPSC repeaters. Required.', show: on('dmr.ipsc.enabled'),
          },
          { path: 'dmr.ipsc.call-hang-hold', label: 'Hang time after voice (seconds)', kind: 'seconds', step: '0.1', help: hangHelp('voice'), show: on('dmr.ipsc.enabled') },
          { path: 'dmr.ipsc.data-hang-hold', label: 'Hang time after data (seconds)', kind: 'seconds', step: '0.1', help: hangHelp('data'), show: on('dmr.ipsc.enabled') },
        ],
      },
    ],
  },
  {
    id: 'openbridge',
    group: 'DMR',
    name: 'OpenBridge',
    intro: [
      'Other servers, such as FreeDMR and HBlink, link with OpenBridge. Add each one on the ',
      { text: 'Peers', to: '/admin/peers' },
      ' page, with its rules.',
    ],
    cards: [
      {
        title: 'OpenBridge server',
        rows: [
          { path: 'dmr.openbridge.enabled', label: 'Accept OpenBridge peers', kind: 'bool' },
          { path: 'dmr.openbridge.bind', label: 'Bind address', kind: 'text', show: on('dmr.openbridge.enabled') },
          { path: 'dmr.openbridge.port', label: 'Port', kind: 'number', help: 'The UDP port of OpenBridge links.', show: on('dmr.openbridge.enabled') },
          {
            path: 'dmr.openbridge.server-id', label: 'Server ID', kind: 'number', show: on('dmr.openbridge.enabled'),
            help: 'The server ID of 4 to 7 digits that DMRHub sends to FreeDMR peers. 0 sends the network ID of each peer.',
          },
        ],
      },
    ],
  },
  {
    id: 'webtransport',
    group: 'DMR',
    name: 'WebTransport',
    intro: ['Hotspots and radios that run in a web browser connect over WebTransport. It uses the bind address of the MMDVM server.'],
    cards: [
      {
        title: 'Browser radios',
        rows: [
          {
            path: 'dmr.mmdvm.webtransport.enabled', label: 'Accept browser hotspots and radios', kind: 'bool',
            help: 'Such as the DMRHub Browser Radio. They log in with a hotspot ID and password, like any other hotspot.',
          },
          {
            path: 'dmr.mmdvm.webtransport.port', label: 'Port', kind: 'number', show: on('dmr.mmdvm.webtransport.enabled'),
            help: 'The UDP port that browsers connect to. It must differ from the MMDVM port.',
          },
          {
            path: 'dmr.mmdvm.webtransport.cert-file', label: 'Certificate file', kind: 'text', placeholder: '/tls/tls.crt',
            show: on('dmr.mmdvm.webtransport.enabled'),
            help: 'Empty makes DMRHub use short-lived certificates of its own, which browsers accept with no setup.',
          },
          {
            path: 'dmr.mmdvm.webtransport.key-file', label: 'Key file', kind: 'text', placeholder: '/tls/tls.key',
            show: on('dmr.mmdvm.webtransport.enabled'), help: 'The key of the certificate file.',
          },
          {
            path: 'dmr.mmdvm.webtransport.allowed-origins', label: 'Allowed web pages', kind: 'list', show: on('dmr.mmdvm.webtransport.enabled'),
            help: 'Origins of the pages that may connect, one for each line, such as https://radio.example.org. Empty allows every page.',
          },
        ],
      },
    ],
  },
  {
    id: 'modem',
    group: 'DMR',
    name: 'Modem',
    intro: [
      'DMRHub can drive an MMDVM modem board, such as an MMDVM_HS hat, an MMDVM repeater board or a DVMega, on a serial port. The board is then a hotspot or repeater of this server, with no MMDVMHost.',
    ],
    saveNote: 'Saving restarts the modem. Calls on it stop for a few seconds.',
    cards: [
      {
        title: 'Modem',
        pill: 'modem',
        rows: [
          { path: 'dmr.modem.enabled', label: 'Enabled', kind: 'bool', help: 'Turn this on in one server only, the one that the board is plugged into.' },
        ],
        showCards: on('dmr.modem.enabled'),
        cards: [
          {
            title: 'Connection',
            rows: [
              {
                grid: [
                  { path: 'dmr.modem.port', label: 'Serial port', kind: 'port', placeholder: '/dev/ttyAMA0' },
                  {
                    path: 'dmr.modem.baud', label: 'Port speed', kind: 'select', help: '115200 for almost every board.',
                    options: [9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600].map((b) => ({ value: b, label: String(b) })),
                  },
                ],
              },
              {
                path: 'dmr.modem.repeater-id', label: 'Repeater ID', kind: 'number', required: true,
                help: 'The ID of this hotspot or repeater on the network. If no repeater has this ID, DMRHub makes one, and an admin assigns its owner.',
              },
              {
                path: 'dmr.modem.duplex', label: 'Mode', kind: 'mode',
                also: (v, read) => (v === true ? [] : [
                  ['dmr.modem.tx-frequency', read('dmr.modem.rx-frequency')],
                  ['dmr.modem.beacons.enabled', false],
                ]),
                help: (read) => (read('dmr.modem.duplex') === true
                  ? 'A repeater uses both timeslots, and repeats what it hears.'
                  : 'A hotspot has one channel, which is timeslot 2 on the network. Calls on timeslot 1 are not sent.'),
              },
            ],
          },
          {
            title: 'Radio',
            rows: [
              {
                grid: [
                  {
                    path: 'dmr.modem.rx-frequency', label: 'Receive frequency (MHz)', kind: 'mhz', required: true,
                    also: (v, read) => (read('dmr.modem.duplex') === true ? [] : [['dmr.modem.tx-frequency', v]]),
                  },
                  { path: 'dmr.modem.tx-frequency', label: 'Transmit frequency (MHz)', kind: 'mhz', required: true, show: on('dmr.modem.duplex') },
                ],
              },
              {
                text: (read) => (read('dmr.modem.duplex') === true
                  ? 'A repeater receives and transmits on its two frequencies.'
                  : 'A hotspot sends and receives on the same frequency.'),
              },
              {
                grid: [
                  {
                    path: 'dmr.modem.color-code', label: 'Color code', kind: 'select', help: 'Radios must use the same color code.',
                    options: Array.from({ length: 16 }, (_, i) => ({ value: i, label: String(i) })),
                  },
                  { path: 'dmr.modem.rf-level', label: 'Transmit power (%)', kind: 'number', step: 'any', help: 'On boards that set their power, such as MMDVM_HS hats.' },
                ],
              },
              {
                grid: [
                  { path: 'dmr.modem.rx-offset', label: 'Receive offset (Hz)', kind: 'number', help: 'Correction for the frequency error of the board.' },
                  { path: 'dmr.modem.tx-offset', label: 'Transmit offset (Hz)', kind: 'number', help: 'Correction for the frequency error of the board.' },
                ],
              },
              {
                details: 'Advanced radio settings',
                rows: [
                  {
                    grid: [
                      { path: 'dmr.modem.rx-level', label: 'Receive level (%)', kind: 'number', step: 'any' },
                      { path: 'dmr.modem.tx-level', label: 'Transmit level (%)', kind: 'number', step: 'any' },
                      { path: 'dmr.modem.rx-dc-offset', label: 'Receive DC offset', kind: 'number' },
                      { path: 'dmr.modem.tx-dc-offset', label: 'Transmit DC offset', kind: 'number' },
                      { path: 'dmr.modem.tx-delay', label: 'Transmit delay (ms)', kind: 'number' },
                      { path: 'dmr.modem.dmr-delay', label: 'Receive delay of a repeater (symbols)', kind: 'number', show: on('dmr.modem.duplex') },
                    ],
                  },
                  { path: 'dmr.modem.rx-invert', label: 'Invert the receive audio', kind: 'bool' },
                  { path: 'dmr.modem.tx-invert', label: 'Invert the transmit audio (most boards need this)', kind: 'bool' },
                  { path: 'dmr.modem.ptt-invert', label: 'Invert the PTT output', kind: 'bool' },
                  {
                    path: 'dmr.modem.rssi-mapping-file', label: 'RSSI mapping file', kind: 'text', placeholder: '/etc/dmrhub/RSSI.dat',
                    help: 'An RSSI.dat file in the MMDVMHost format, which turns the signal reading of the board into dBm. Without it the signal strength of calls is not shown.',
                  },
                  { path: 'dmr.modem.debug', label: 'Log the debug messages of the board', kind: 'bool' },
                ],
              },
            ],
          },
          {
            title: 'Station ID',
            rows: [
              {
                path: 'dmr.modem.cw-id.enabled', label: 'Send a morse station ID', kind: 'bool',
                help: 'The ID goes out only while the channel is idle. The first goes out a quarter of the time after the board connects or a call ends.',
              },
              {
                grid: [
                  {
                    path: 'dmr.modem.cw-id.callsign', label: 'Callsign', kind: 'text', uppercase: true, required: true,
                    help: 'Letters, digits, spaces and / ? , - = .', show: on('dmr.modem.cw-id.enabled'),
                  },
                  { path: 'dmr.modem.cw-id.time', label: 'Minutes between IDs', kind: 'number', help: '1 to 1440.', show: on('dmr.modem.cw-id.enabled') },
                ],
              },
            ],
          },
          {
            title: 'Beacons',
            rows: [
              {
                path: 'dmr.modem.beacons.enabled', label: 'Key up the transmitter at intervals while the channel is idle', kind: 'bool',
                off: (read) => read('dmr.modem.duplex') !== true,
                help: (read) => (read('dmr.modem.duplex') === true
                  ? 'Beacons let radios that roam find the repeater.'
                  : 'Beacons let radios that roam find the repeater. Only a repeater sends them; choose Repeater mode to turn them on.'),
              },
              {
                grid: [
                  {
                    path: 'dmr.modem.beacons.interval', label: 'Seconds between beacons', kind: 'number',
                    show: (read) => read('dmr.modem.duplex') === true && read('dmr.modem.beacons.enabled') === true,
                  },
                  {
                    path: 'dmr.modem.beacons.duration', label: 'Seconds per beacon', kind: 'number',
                    show: (read) => read('dmr.modem.duplex') === true && read('dmr.modem.beacons.enabled') === true,
                  },
                ],
              },
            ],
          },
          { title: 'Test the modem', rows: [], custom: 'modem-test' },
        ],
      },
    ],
  },
  {
    id: 'transcoding',
    group: 'DMR',
    name: 'Transcoding',
    intro: ['AMBE servers convert calls between Codec2 and AMBE. With no server, calls pass through in their own codec.'],
    custom: 'transcoding',
    cards: [],
  },
  {
    id: 'positions',
    group: 'Features',
    name: 'Positions and APRS',
    intro: ['Owners turn on position tracking for each radio. These are the limits, and the APRS-IS login that sends the positions on.'],
    cards: [
      {
        title: 'Limits for owners',
        rows: [
          {
            path: 'dmr.positions.min-interval', label: 'Shortest report interval', kind: 'select',
            help: 'The shortest interval that an owner can pick for a radio.',
            options: [5, 10, 15, 30, 60].map((m) => ({ value: m, label: `${m} minutes` })),
          },
          {
            path: 'dmr.positions.max-retention', label: 'Longest retention (days)', kind: 'number',
            help: 'The longest time that an owner can keep the positions of a radio, 1 to 365.',
          },
        ],
      },
      {
        title: 'APRS-IS',
        rows: [
          {
            path: 'dmr.positions.aprs-is.callsign', label: 'APRS-IS callsign', kind: 'text',
            help: 'The callsign with SSID that the hub logs in to APRS-IS as. Empty sends nothing to APRS-IS. DMRHub makes the passcode from the callsign.',
          },
          { path: 'dmr.positions.aprs-is.server', label: 'APRS-IS server', kind: 'text', help: 'As host:port.' },
        ],
      },
    ],
  },
  {
    id: 'messages',
    group: 'Features',
    name: 'Messages',
    intro: ['Text and data messages between radios.'],
    cards: [
      {
        title: 'Messages',
        rows: [
          {
            path: 'dmr.retention.messages', label: 'Keep messages (days)', kind: 'number',
            help: 'Text and data messages, with their raw bytes. 0 keeps them forever.',
          },
        ],
      },
    ],
  },
  {
    id: 'plugins',
    group: 'Features',
    name: 'Plugins',
    intro: [
      'Plugins answer the calls to the IDs that an admin gives them. Add and assign them on the ',
      { text: 'Plugins', to: '/admin/plugins' },
      ' page.',
    ],
    cards: [
      {
        title: 'Where plugins run',
        rows: [
          {
            path: 'dmr.plugins.in-process', label: 'Run plugins inside DMRHub', kind: 'bool',
            help: 'The parrot, the location server and the uploaded plugins. Turn it off only when every plugin runs as its own program.',
          },
        ],
      },
      {
        title: 'Plugin listener',
        rows: [
          { path: 'dmr.plugins.enabled', label: 'Accept plugins that run as their own programs', kind: 'bool' },
          { path: 'dmr.plugins.bind', label: 'Bind address', kind: 'text' },
          { path: 'dmr.plugins.port', label: 'Port', kind: 'number', help: 'The TCP port that plugins connect to.' },
          {
            path: 'dmr.plugins.cert-file', label: 'Certificate file', kind: 'text',
            help: 'Empty makes DMRHub use a certificate of its own, which plugins check by its fingerprint.',
          },
          { path: 'dmr.plugins.key-file', label: 'Key file', kind: 'text' },
        ],
      },
    ],
  },
  {
    id: 'pcroute',
    group: 'Features',
    name: 'Private call routing',
    optional: true,
    intro: ['Where DMRHub sends a call or message to one radio. It goes to the repeater or hotspot where DMRHub last heard that radio.'],
    cards: [
      {
        title: 'Voice calls',
        rows: [
          { path: 'dmr.private-calls.voice.reach', label: 'Who a private call can reach', kind: 'choice', options: reach },
          { path: 'dmr.private-calls.voice.unknown', label: 'When DMRHub does not know where the radio is', kind: 'choice', options: unknown },
          {
            path: 'dmr.private-calls.voice.max-age', label: 'Last heard age limit (minutes)', kind: 'minutes',
            help: 'A call goes to the place where DMRHub last heard the radio. Set a limit to treat radios that DMRHub has not heard for longer as unknown. 0 is no limit.',
          },
        ],
      },
      {
        title: 'Text messages and data',
        rows: [
          { path: 'dmr.private-calls.data.reach', label: 'Who a text message can reach', kind: 'choice', options: [sameAsVoice, ...reach] },
          { path: 'dmr.private-calls.data.unknown', label: 'When DMRHub does not know where the radio is', kind: 'choice', options: [sameAsVoice, ...unknown] },
          {
            path: 'dmr.private-calls.data.max-age', label: 'Last heard age limit (minutes)', kind: 'minutes',
            help: '0 uses the limit of voice calls. A negative number is no limit.',
          },
        ],
      },
    ],
  },
];

/** The paths under obj, read one part of path at a time. */
export const getAt = (obj: unknown, path: string): unknown => {
  let cur: unknown = obj;
  for (const part of path.split('.')) {
    if (typeof cur !== 'object' || cur === null || Array.isArray(cur)) {
      return undefined;
    }
    cur = (cur as Record<string, unknown>)[part];
  }
  return cur;
};

/** Sets the value at path in obj, with the objects on the way. */
export const setAt = (obj: Values, path: string, value: unknown): void => {
  const parts = path.split('.');
  let cur: Values = obj;
  for (const part of parts.slice(0, -1)) {
    const next = cur[part];
    if (typeof next !== 'object' || next === null || Array.isArray(next)) {
      cur[part] = {};
    }
    cur = cur[part] as Values;
  }
  cur[parts[parts.length - 1] as string] = value;
};

export const clone = <T>(v: T): T => (v === undefined ? v : JSON.parse(JSON.stringify(v)) as T);

const rowFields = (rows: Row[]): FieldDef[] => rows.flatMap((r) => {
  if (isGrid(r)) {
    return r.grid;
  }
  if (isDetails(r)) {
    return rowFields(r.rows);
  }
  if (isField(r)) {
    return [r];
  }
  return [];
});

const cardFields = (cards: CardDef[]): FieldDef[] =>
  cards.flatMap((c) => [...rowFields(c.rows), ...cardFields(c.cards ?? [])]);

/** The settings of a tab. */
export const tabFields = (tab: TabDef): FieldDef[] => cardFields(tab.cards);

export const AMBE_SERVERS = 'dmr.transcoding.ambe-servers';

/** The paths of the settings that the Save of a tab saves. */
export const tabPaths = (tab: TabDef): string[] =>
  tab.custom === 'transcoding' ? [AMBE_SERVERS] : tabFields(tab).map((f) => f.path);

/** The tabs that the menu shows: an optional tab needs its settings. */
export const visibleTabs = (state: SetupState): TabDef[] =>
  TABS.filter((t) => !t.optional || tabPaths(t).some((p) => p in state.settings));

/** The tab of a URL hash, such as #ipsc; the first tab for any other. */
export const tabOfHash = (hash: string, tabs: TabDef[]): TabDef => {
  const id = hash.replace(/^#/, '');
  return tabs.find((t) => t.id === id) ?? (tabs[0] as TabDef);
};

const numeric = (f: FieldDef) => f.options !== undefined && typeof f.options[0]?.value === 'number';

const toNumber = (v: unknown): number => {
  const n = typeof v === 'number' ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
};

/** The value of a field as the config API takes it. */
export const normalize = (f: FieldDef, v: unknown): unknown => {
  switch (f.kind) {
    case 'bool':
    case 'mode':
      return v === true;
    case 'number':
    case 'seconds':
    case 'minutes':
    case 'mhz':
      return toNumber(v);
    case 'list':
      return Array.isArray(v) ? v.map((e) => String(e).trim()).filter((e) => e !== '') : [];
    case 'select':
    case 'choice':
      if (numeric(f)) {
        return toNumber(v);
      }
      return v === undefined || v === null ? '' : String(v);
    default:
      return v === undefined || v === null ? '' : String(v);
  }
};

type AmbeServerValue = { address: string; calls: number };

const ambeServersOf = (v: unknown): AmbeServerValue[] =>
  Array.isArray(v)
    ? ambeServersPayload(v.map((r) => {
      const row = (typeof r === 'object' && r !== null ? r : {}) as Partial<AmbeServerRow>;
      return { address: String(row.address ?? ''), calls: row.calls ?? 0 };
    }))
    : [];

const fieldOf = (tab: TabDef, path: string): FieldDef | undefined => tabFields(tab).find((f) => f.path === path);

const valueFor = (tab: TabDef, path: string, values: Values): unknown => {
  if (path === AMBE_SERVERS) {
    return ambeServersOf(getAt(values, path));
  }
  const f = fieldOf(tab, path);
  return f ? normalize(f, getAt(values, path)) : getAt(values, path);
};

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/** Whether the page holds changes to the settings of tab that are not saved. */
export const tabDirty = (tab: TabDef, original: Values, edited: Values): boolean =>
  tabPaths(tab).some((p) => !same(valueFor(tab, p, original), valueFor(tab, p, edited)));

/**
 * The config to save for tab: the saved config, with the values of the page
 * for the settings of tab only.
 */
export const tabPayload = (tab: TabDef, original: Values, edited: Values): Values => {
  const out = clone(original);
  for (const p of tabPaths(tab)) {
    setAt(out, p, valueFor(tab, p, edited));
  }
  return out;
};

/**
 * The values of fresh, with the values of edited for the settings of the
 * dirty tabs other than saved, so that a Save keeps the changes on the
 * other tabs.
 */
export const keepEdits = (fresh: Values, original: Values, edited: Values, saved: TabDef | null): Values => {
  const out = clone(fresh);
  for (const t of TABS) {
    if (t === saved || !tabDirty(t, original, edited)) {
      continue;
    }
    for (const p of tabPaths(t)) {
      setAt(out, p, clone(getAt(edited, p)));
    }
  }
  return out;
};

export type ChipKind = 'default' | 'setup' | 'lock' | 'boot' | 'generated' | 'wizard' | 'differ' | 'not-in-effect';
export type Chip = { kind: ChipKind; text: string; code?: string };

/** The chips that say where the value of a setting comes from. */
export const sourceChips = (s: Setting | undefined): Chip[] => {
  if (!s) {
    return [];
  }
  const out: Chip[] = [];
  switch (s.source) {
    case 'env':
      out.push({ kind: 'lock', text: 'Environment', code: s.origin });
      break;
    case 'flag':
      out.push({ kind: 'lock', text: 'Command line', code: s.origin });
      break;
    case 'file':
      out.push({ kind: 'lock', text: 'Config file' });
      break;
    case 'setup':
      out.push({ kind: 'setup', text: 'Saved in Setup' });
      break;
    case 'generated':
      out.push({ kind: 'generated', text: 'Made by DMRHub' });
      break;
    case 'wizard':
      out.push({ kind: 'wizard', text: 'Set by the setup wizard' });
      break;
    default:
      out.push({ kind: 'default', text: 'Default' });
  }
  if (s.start_only && !s.locked && s.source !== 'generated' && s.source !== 'wizard') {
    out.push({ kind: 'boot', text: 'Read at start' });
  }
  if (s.disagree) {
    out.push({ kind: 'differ', text: 'Replicas differ' });
  }
  if (s.not_in_effect) {
    out.push({ kind: 'not-in-effect', text: 'Not in effect' });
  }
  return out;
};

/** Whether Setup cannot change the setting; one it does not know it can. */
export const fieldLocked = (s: Setting | undefined): boolean => s !== undefined && !s.editable;

/** Whether nothing on tab can change while DMRHub runs. */
export const tabReadOnly = (tab: TabDef, state: SetupState): boolean => {
  const paths = tabPaths(tab).filter((p) => p in state.settings);
  return paths.length > 0 && paths.every((p) => fieldLocked(state.settings[p]));
};

/** Whether every setting of tab takes effect at start only. */
export const tabStartOnly = (tab: TabDef, state: SetupState): boolean => {
  const paths = tabPaths(tab).filter((p) => p in state.settings);
  return paths.length > 0 && paths.every((p) => state.settings[p]?.start_only === true);
};

/** A value of a field, as the notes of the Setup page show it. */
export const shownValue = (f: FieldDef, v: unknown): string => {
  const n = normalize(f, v);
  switch (f.kind) {
    case 'seconds':
      return `${(n as number) / 1e9} seconds`;
    case 'minutes':
      return `${(n as number) / 6e10} minutes`;
    case 'mhz':
      return `${((n as number) / 1e6).toFixed(4)} MHz`;
    case 'mode':
      return n ? 'Repeater (duplex)' : 'Hotspot (simplex)';
    case 'select':
    case 'choice':
      return f.options?.find((o) => o.value === n)?.label ?? formatValue(n);
    default:
      return formatValue(n);
  }
};

/**
 * The default of a setting saved in Setup, as the "Use the default" note
 * shows it, while the value on the page differs from it; undefined when
 * the note does not show.
 */
export const defaultNote = (f: FieldDef, s: Setting | undefined, value: unknown): string | undefined => {
  if (f.required || !s || !s.editable || s.source !== 'setup' || s.default === undefined) {
    return undefined;
  }
  if (same(normalize(f, value), normalize(f, s.default))) {
    return undefined;
  }
  return shownValue(f, s.default);
};

/** The value of an input for the value of a field. */
export const inputValue = (f: FieldDef, v: unknown): string => {
  switch (f.kind) {
    case 'seconds':
      return String(toNumber(v) / 1e9);
    case 'minutes':
      return String(toNumber(v) / 6e10);
    case 'mhz':
      return toNumber(v) === 0 ? '' : (toNumber(v) / 1e6).toFixed(4);
    case 'list':
      return Array.isArray(v) ? v.join('\n') : '';
    case 'number':
      return v === undefined || v === null || v === '' ? '0' : String(v);
    default:
      return v === undefined || v === null ? '' : String(v);
  }
};

/** The value of a field for the text of its input. */
export const fromInput = (f: FieldDef, text: string): unknown => {
  switch (f.kind) {
    case 'seconds':
      return Math.round(Number(text) * 1e9) || 0;
    case 'minutes':
      return Math.round(Number(text) * 6e10) || 0;
    case 'mhz':
      return Math.round(Number(text) * 1e6) || 0;
    case 'list':
      return text.split('\n');
    case 'number':
      return text === '' ? '' : Number(text);
    case 'select':
    case 'choice':
      return numeric(f) ? Number(text) : text;
    default:
      return f.uppercase ? text.toUpperCase() : text;
  }
};
