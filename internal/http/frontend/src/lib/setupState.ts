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

import type { InjectionKey } from 'vue';

export type SettingSource = 'default' | 'setup' | 'file' | 'env' | 'flag' | 'generated' | 'wizard';

export type ReplicaValue = {
  replica: string;
  self: boolean;
  value: unknown;
  source: SettingSource;
};

export type Setting = {
  editable: boolean;
  locked: boolean;
  start_only: boolean;
  source: SettingSource;
  origin?: string;
  saved?: unknown;
  default?: unknown;
  not_in_effect: boolean;
  disagree: boolean;
  replicas?: ReplicaValue[];
};

export type ApplyError = { server: string; running: boolean; error: string };
export type FieldError = { field: string; error: string };

export type ReplicaStatus = {
  id: string;
  self: boolean;
  revision: number;
  current: boolean;
  at: string;
  apply_errors: ApplyError[] | null;
  invalid: FieldError[] | null;
};

export type WizardStatus = {
  phase: 'database' | 'setup' | 'done';
  database_error?: string;
  database_locked?: boolean;
  database_saved?: boolean;
  database_file?: string;
  store_error?: string;
};

export type SetupState = {
  loaded: boolean;
  settings: Record<string, Setting>;
  replicas: ReplicaStatus[];
  disagree: boolean;
  revision: number;
  wizard: WizardStatus | null;
};

export const setupStateKey: InjectionKey<SetupState> = Symbol('setupState');

export const emptySetupState = (): SetupState => ({
  loaded: false,
  settings: {},
  replicas: [],
  disagree: false,
  revision: 0,
  wizard: null,
});

/** The keys of a config response that describe the settings, not the config. */
export const stateKeys = ['settings', 'replicas', 'disagree', 'revision', 'wizard', 'secrets'];

const asRecord = (v: unknown): Record<string, unknown> | undefined =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined;

/** Reads the setting state out of a config response. */
export const setupStateOf = (data: unknown): SetupState => {
  const d = asRecord(data) ?? {};
  return {
    loaded: true,
    settings: (asRecord(d.settings) as Record<string, Setting> | undefined) ?? {},
    replicas: Array.isArray(d.replicas) ? (d.replicas as ReplicaStatus[]) : [],
    disagree: d.disagree === true,
    revision: typeof d.revision === 'number' ? d.revision : 0,
    wizard: (asRecord(d.wizard) as WizardStatus | undefined) ?? null,
  };
};

/** Whether Setup cannot change the setting at path. */
export const settingLocked = (state: SetupState | null | undefined, path: string): boolean => {
  const s = state?.settings[path];
  return s !== undefined && !s.editable;
};

/** The short name of a replica, as Setup shows it. */
export const replicaName = (id: string): string => id.slice(0, 4);

const servers: Record<string, string> = { mmdvm: 'MMDVM', ipsc: 'IPSC', openbridge: 'OpenBridge', modem: 'modem' };

/** The name of a DMR server, as Setup shows it. */
export const serverName = (server: string): string => servers[server] ?? 'DMR';

const labels: Record<string, string> = {
  'log-level': 'Log Level',
  'network-name': 'Network Name',
  'auto-approve-users': 'Approve new users automatically',
  'secret': 'Secret',
  'password-salt': 'Password Salt',
  'hibp-api-key': 'HaveIBeenPwned API Key',
  'http.bind': 'Bind Address (HTTP)',
  'http.port': 'Port (HTTP)',
  'http.trusted-proxies': 'Trusted Proxies (HTTP)',
  'http.canonical-host': 'Canonical Host',
  'http.cors.enabled': 'CORS',
  'http.cors.extra-hosts': 'CORS Hosts',
  'http.robots-txt.mode': 'robots.txt Mode',
  'http.robots-txt.content': 'robots.txt Content',
  'database.driver': 'Database Driver',
  'database.database': 'Database',
  'database.host': 'Database Host',
  'database.port': 'Database Port',
  'database.username': 'Database Username',
  'database.password': 'Database Password',
  'database.extra-parameters': 'Database Parameters',
  'redis.enabled': 'Redis',
  'redis.host': 'Redis Host',
  'redis.port': 'Redis Port',
  'redis.password': 'Redis Password',
  'smtp.enabled': 'Email',
  'smtp.host': 'SMTP Host',
  'smtp.port': 'SMTP Port',
  'smtp.tls': 'SMTP TLS',
  'smtp.username': 'SMTP Username',
  'smtp.password': 'SMTP Password',
  'smtp.from': 'From Address',
  'smtp.auth-method': 'SMTP Authentication',
  'metrics.enabled': 'Metrics',
  'metrics.bind': 'Bind Address (metrics)',
  'metrics.port': 'Port (metrics)',
  'metrics.trusted-proxies': 'Trusted Proxies (metrics)',
  'metrics.otlp-endpoint': 'OTLP Endpoint',
  'pprof.enabled': 'PProf',
  'pprof.bind': 'Bind Address (PProf)',
  'pprof.port': 'Port (PProf)',
  'pprof.trusted-proxies': 'Trusted Proxies (PProf)',
  'dmr.mmdvm.bind': 'Bind Address (MMDVM)',
  'dmr.mmdvm.port': 'Port (MMDVM)',
  'dmr.mmdvm.webtransport.enabled': 'Browser radios',
  'dmr.mmdvm.webtransport.port': 'Port (browser radios)',
  'dmr.mmdvm.webtransport.cert-file': 'Certificate File (browser radios)',
  'dmr.mmdvm.webtransport.key-file': 'Key File (browser radios)',
  'dmr.ipsc.enabled': 'IPSC',
  'dmr.ipsc.bind': 'Bind Address (IPSC)',
  'dmr.ipsc.port': 'Port (IPSC)',
  'dmr.ipsc.network-id': 'Network ID (IPSC)',
  'dmr.ipsc.call-hang-hold': 'Hang time after voice (IPSC)',
  'dmr.ipsc.data-hang-hold': 'Hang time after data (IPSC)',
  'dmr.modem.enabled': 'Modem',
  'dmr.modem.port': 'Serial port (modem)',
  'dmr.modem.baud': 'Speed (modem)',
  'dmr.modem.repeater-id': 'Repeater ID (modem)',
  'dmr.modem.duplex': 'Duplex (modem)',
  'dmr.modem.color-code': 'Color code (modem)',
  'dmr.modem.rx-frequency': 'Receive frequency (modem)',
  'dmr.modem.tx-frequency': 'Transmit frequency (modem)',
  'dmr.modem.rx-offset': 'Receive offset (modem)',
  'dmr.modem.tx-offset': 'Transmit offset (modem)',
  'dmr.modem.rf-level': 'RF level (modem)',
  'dmr.modem.rx-level': 'Receive level (modem)',
  'dmr.modem.tx-level': 'Transmit level (modem)',
  'dmr.modem.rx-dc-offset': 'Receive DC offset (modem)',
  'dmr.modem.tx-dc-offset': 'Transmit DC offset (modem)',
  'dmr.modem.rx-invert': 'Invert receive (modem)',
  'dmr.modem.tx-invert': 'Invert transmit (modem)',
  'dmr.modem.ptt-invert': 'Invert PTT (modem)',
  'dmr.modem.tx-delay': 'Transmit delay (modem)',
  'dmr.modem.dmr-delay': 'DMR delay (modem)',
  'dmr.modem.debug': 'Debug messages (modem)',
  'dmr.openbridge.enabled': 'OpenBridge',
  'dmr.openbridge.bind': 'Bind Address (OpenBridge)',
  'dmr.openbridge.port': 'Port (OpenBridge)',
  'dmr.openbridge.server-id': 'Server ID (OpenBridge)',
  'dmr.disable-radio-id-validation': 'Turn off all RadioID checks',
  'dmr.radio-id-checks.user-id': 'DMR ID has 7 digits',
  'dmr.radio-id-checks.callsign': 'Callsign matches DMR ID',
  'dmr.radio-id-checks.radio-ids': 'Added radio IDs',
  'dmr.radio-id-checks.repeater-ids': 'Repeater IDs',
  'dmr.radio-id-checks.hotspot-ids': 'Hotspot IDs start with owner ID',
  'registration': 'Who can register',
  'login-backoff': 'Pause an account after 10 failed logins',
  'http.session-length': 'Login lasts',
  'visibility': 'Visitors who are not logged in see',
  'dmr.radio-id-url': 'Radio ID URL',
  'dmr.repeater-id-url': 'Repeater ID URL',
  'dmr.public-address': 'Public server address',
  'dmr.retention.messages': 'Keep messages',
  'dmr.retention.transmissions': 'Keep last heard',
  'dmr.retention.bursts': 'Keep raw bursts',
  'dmr.positions.min-interval': 'Shortest report interval',
  'dmr.positions.max-retention': 'Longest position retention',
  'dmr.positions.aprs-is.callsign': 'APRS-IS Callsign',
  'dmr.positions.aprs-is.server': 'APRS-IS Server',
  'dmr.plugins.in-process': 'Plugins inside DMRHub',
  'dmr.plugins.enabled': 'Plugins as their own programs',
  'dmr.plugins.bind': 'Bind Address (plugins)',
  'dmr.plugins.port': 'Port (plugins)',
  'dmr.plugins.cert-file': 'Certificate File (plugins)',
  'dmr.plugins.key-file': 'Key File (plugins)',
  'dmr.transcoding.ambe-servers': 'AMBE servers',
  'dmr.drain-timeout': 'Drain Timeout',
};

/** The name of the setting at path, as Setup shows it; never the path. */
export const settingLabel = (path: string): string => labels[path] ?? 'a setting';

/** A value of a setting, as Setup shows it. */
export const formatValue = (v: unknown): string => {
  if (v === null || v === undefined || v === '') {
    return 'empty';
  }
  if (typeof v === 'boolean') {
    return v ? 'on' : 'off';
  }
  if (Array.isArray(v)) {
    if (v.length === 0) {
      return 'none';
    }
    return v.map((e) => {
      const r = asRecord(e);
      return r && typeof r.address === 'string' ? r.address : String(e);
    }).join(', ');
  }
  return String(v);
};

const sources: Record<SettingSource, string> = {
  default: 'the default',
  setup: 'saved in Setup',
  file: 'set by its config file',
  env: 'set by its environment',
  flag: 'set by its command line',
  generated: 'made by DMRHub',
  wizard: 'set by the setup wizard',
};

/** Where the value of a replica comes from, as Setup shows it. */
export const sourceText = (s: SettingSource): string => sources[s] ?? 'the default';

const deployments: Partial<Record<SettingSource, string>> = {
  file: 'config file',
  env: 'environment',
  flag: 'command line',
};

/** The part of the deployment that sets a locked setting. */
export const deploymentPart = (s: SettingSource): string => deployments[s] ?? 'deployment';

/** Whether the value of a replica is not the one it should run with. */
export const replicaNotInEffect = (setting: Setting, r: ReplicaValue): boolean => {
  if (r.source !== 'setup' && r.source !== 'default') {
    return false;
  }
  const want = setting.saved !== undefined ? setting.saved : setting.default;
  return want !== undefined && JSON.stringify(want) !== JSON.stringify(r.value);
};
