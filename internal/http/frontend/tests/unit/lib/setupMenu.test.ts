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
import {
  TABS, defaultNote, fromInput, inputValue, keepEdits, sourceChips, tabCheckErrors, tabDirty, tabFields, tabOfHash, tabPaths, tabPayload, tabReadOnly,
  tabStartOnly, visibleTabs, type FieldDef, type TabDef,
} from '@/lib/setupMenu';
import { emptySetupState, type Setting, type SetupState } from '@/lib/setupState';

const tab = (id: string): TabDef => TABS.find((t) => t.id === id) as TabDef;
const field = (path: string): FieldDef => TABS.flatMap(tabFields).find((f) => f.path === path) as FieldDef;

const plain: Setting = { editable: true, locked: false, start_only: false, source: 'default', not_in_effect: false, disagree: false };
const boot: Setting = { ...plain, editable: false, start_only: true };

const stateWith = (settings: Record<string, Setting>): SetupState => ({ ...emptySetupState(), loaded: true, settings });

describe('setup menu tabs', () => {
  it('splits General and HTTP and keeps Metrics and PProf', () => {
    expect(TABS.map((t) => t.id)).toEqual([
      'general', 'accounts', 'http', 'database', 'redis', 'smtp', 'metrics', 'dmr', 'ipsc', 'openbridge', 'webtransport', 'modem',
      'transcoding', 'iplists', 'routing', 'positions', 'messages', 'plugins', 'pcroute',
    ]);
    expect(tabPaths(tab('general'))).toEqual(['network-name', 'log-level', 'auto-approve-users', 'secret', 'password-salt', 'hibp-api-key']);
    expect(tabPaths(tab('http'))).toContain('http.canonical-host');
    expect(tabPaths(tab('http'))).toContain('http.cors.extra-hosts');
    expect(tabPaths(tab('metrics'))).toEqual(expect.arrayContaining(['metrics.enabled', 'pprof.enabled']));
    expect(tabPaths(tab('dmr'))).toContain('dmr.public-address');
    expect(tabPaths(tab('transcoding'))).toEqual(['dmr.transcoding.ambe-servers']);
  });

  it('puts each setting on one tab only', () => {
    const paths = TABS.flatMap(tabPaths);
    expect(new Set(paths).size).toBe(paths.length);
  });

  it('never labels a field with its config key', () => {
    const fields = TABS.flatMap(tabFields);
    for (const f of fields) {
      for (const p of fields.map((g) => g.path)) {
        expect(f.label).not.toContain(p);
      }
    }
  });

  it('routes a hash to its tab and anything else to the first tab', () => {
    const tabs = visibleTabs(stateWith({}));
    expect(tabOfHash('#ipsc', tabs).id).toBe('ipsc');
    expect(tabOfHash('modem', tabs).id).toBe('modem');
    expect(tabOfHash('', tabs).id).toBe('general');
    expect(tabOfHash('#nope', tabs).id).toBe('general');
  });

  it('shows the private call tab only when its settings exist', () => {
    expect(visibleTabs(stateWith({})).map((t) => t.id)).not.toContain('pcroute');
    expect(tabOfHash('#pcroute', visibleTabs(stateWith({}))).id).toBe('general');
    const withRoute = stateWith({ 'dmr.private-calls.voice.reach': plain });
    expect(visibleTabs(withRoute).map((t) => t.id)).toContain('pcroute');
    expect(tabOfHash('#pcroute', visibleTabs(withRoute)).id).toBe('pcroute');
  });
});

describe('dirty tracking and saves', () => {
  const original = {
    'network-name': 'Example', 'log-level': 'info',
    dmr: { ipsc: { enabled: true, port: 50000, 'call-hang-hold': 2500000000 }, transcoding: { 'ambe-servers': [{ address: 'a:2460', calls: 1 }] } },
    http: { 'trusted-proxies': ['10.0.0.1'] },
  };

  it('is clean until a value of the tab changes', () => {
    const edited = structuredClone(original);
    expect(tabDirty(tab('general'), original, edited)).toBe(false);
    edited['network-name'] = 'Other';
    expect(tabDirty(tab('general'), original, edited)).toBe(true);
    expect(tabDirty(tab('ipsc'), original, edited)).toBe(false);
  });

  it('treats a missing zero value as equal to an explicit zero', () => {
    const edited = structuredClone(original) as Record<string, unknown>;
    (edited.dmr as Record<string, unknown>).openbridge = { enabled: false, port: 0 };
    expect(tabDirty(tab('openbridge'), original, edited)).toBe(false);
  });

  it('ignores empty lines and spaces in a list', () => {
    const edited = structuredClone(original);
    edited.http['trusted-proxies'] = ['10.0.0.1 ', ''];
    expect(tabDirty(tab('http'), original, edited)).toBe(false);
  });

  it('compares AMBE servers as the API takes them', () => {
    const edited = structuredClone(original) as Record<string, unknown>;
    (edited.dmr as { transcoding: unknown }).transcoding = { 'ambe-servers': [{ address: 'a:2460 ', calls: '1' }] };
    expect(tabDirty(tab('transcoding'), original, edited)).toBe(false);
    (edited.dmr as { transcoding: unknown }).transcoding = { 'ambe-servers': [] };
    expect(tabDirty(tab('transcoding'), original, edited)).toBe(true);
  });

  it('saves only the settings of the tab', () => {
    const edited = structuredClone(original);
    edited['network-name'] = 'Other';
    edited.dmr.ipsc.port = 50010;
    const general = tabPayload(tab('general'), original, edited);
    expect(general['network-name']).toBe('Other');
    expect((general.dmr as typeof original.dmr).ipsc.port).toBe(50000);
    const ipsc = tabPayload(tab('ipsc'), original, edited);
    expect(ipsc['network-name']).toBe('Example');
    expect((ipsc.dmr as typeof original.dmr).ipsc.port).toBe(50010);
  });

  it('keeps the unsaved changes of the other tabs after a save', () => {
    const edited = structuredClone(original);
    edited['network-name'] = 'Other';
    edited.dmr.ipsc.port = 50010;
    const fresh = structuredClone(original);
    fresh['network-name'] = 'Other';
    const kept = keepEdits(fresh, original, edited, tab('general')) as typeof original;
    expect(kept['network-name']).toBe('Other');
    expect(kept.dmr.ipsc.port).toBe(50010);
  });
});

describe('source chips and locks', () => {
  it('names the environment variable and the flag of a locked setting', () => {
    expect(sourceChips({ ...plain, editable: false, locked: true, source: 'env', origin: 'DMR_MODEM_PORT' }))
      .toEqual([{ kind: 'lock', text: 'Environment', code: 'DMR_MODEM_PORT' }]);
    expect(sourceChips({ ...plain, editable: false, locked: true, source: 'flag', origin: '--http.port' }))
      .toEqual([{ kind: 'lock', text: 'Command line', code: '--http.port' }]);
    expect(sourceChips({ ...plain, editable: false, locked: true, source: 'file' }))
      .toEqual([{ kind: 'lock', text: 'Config file' }]);
  });

  it('marks saved, default and start-only settings', () => {
    expect(sourceChips({ ...plain, source: 'setup' }).map((c) => c.text)).toEqual(['Saved in Setup']);
    expect(sourceChips(plain).map((c) => c.text)).toEqual(['Default']);
    expect(sourceChips(boot).map((c) => c.text)).toEqual(['Default', 'Read at start']);
    expect(sourceChips({ ...boot, locked: true, source: 'env', origin: 'SECRET' }).map((c) => c.text)).toEqual(['Environment']);
    expect(sourceChips({ ...plain, source: 'setup', disagree: true, not_in_effect: true }).map((c) => c.kind))
      .toEqual(['setup', 'differ', 'not-in-effect']);
  });

  it('calls a tab read only when nothing on it can change', () => {
    const settings = Object.fromEntries(tabPaths(tab('database')).map((p) => [p, boot]));
    expect(tabReadOnly(tab('database'), stateWith(settings))).toBe(true);
    expect(tabStartOnly(tab('database'), stateWith(settings))).toBe(true);
    expect(tabReadOnly(tab('general'), stateWith({ ...settings, 'network-name': plain }))).toBe(false);
  });

  it('offers the default for a saved value that differs from it', () => {
    const saved: Setting = { ...plain, source: 'setup', saved: 'Example', default: 'DMRHub' };
    expect(defaultNote(field('network-name'), saved, 'Example')).toBe('DMRHub');
    expect(defaultNote(field('network-name'), saved, 'DMRHub')).toBeUndefined();
    expect(defaultNote(field('dmr.modem.repeater-id'), { ...plain, source: 'setup', default: 0 }, 300000102)).toBeUndefined();
    expect(defaultNote(field('network-name'), { ...saved, source: 'default' }, 'Example')).toBeUndefined();
    expect(defaultNote(field('network-name'), { ...saved, editable: false, locked: true, source: 'env' }, 'Example')).toBeUndefined();
    const hang: Setting = { ...plain, source: 'setup', default: 2500000000 };
    expect(defaultNote(field('dmr.ipsc.call-hang-hold'), hang, 1000000000)).toBe('2.5 seconds');
    expect(defaultNote(field('dmr.modem.duplex'), { ...plain, source: 'setup', default: false }, true)).toBe('Hotspot (simplex)');
  });
});

describe('setup menu IP lists', () => {
  const lists = ['mmdvm', 'webtransport', 'ipsc', 'openbridge'];
  const reader = (values: Record<string, unknown>) => (p: string) => values[p];

  it('has a card with a mode and two lists for each protocol, and no explainer text', () => {
    const t = tab('iplists');
    expect(t.intro).toEqual([]);
    expect(t.cards.map((c) => c.title)).toEqual(['MMDVM hotspots and repeaters', 'Browser radios', 'Motorola IPSC repeaters', 'OpenBridge peers']);
    expect(tabPaths(t)).toEqual(lists.flatMap((l) => [`dmr.ip-lists.${l}.mode`, `dmr.ip-lists.${l}.block`, `dmr.ip-lists.${l}.allow`]));
    expect(field('dmr.ip-lists.ipsc.mode').options?.map((o) => o.label)).toEqual(['Off', 'Block listed', 'Allow only listed']);
    for (const f of tabFields(t)) {
      expect(f.help).toBeUndefined();
    }
  });

  it('shows only the list of the mode', () => {
    const block = field('dmr.ip-lists.mmdvm.block');
    const allow = field('dmr.ip-lists.mmdvm.allow');
    for (const [mode, b, a] of [['off', false, false], ['block', true, false], ['allow', false, true]] as const) {
      const read = reader({ 'dmr.ip-lists.mmdvm.mode': mode });
      expect({ mode, block: block.show?.(read), allow: allow.show?.(read) }).toEqual({ mode, block: b, allow: a });
    }
  });

  it('checks the entries of the shown list only', () => {
    const t = tab('iplists');
    const values = {
      'dmr.ip-lists.mmdvm.mode': 'block', 'dmr.ip-lists.mmdvm.block': ['203.0.113.7', '10.1.2.3/8'],
      'dmr.ip-lists.webtransport.mode': 'off', 'dmr.ip-lists.webtransport.block': ['nope'],
      'dmr.ip-lists.ipsc.mode': 'allow', 'dmr.ip-lists.ipsc.allow': ['192.0.2.0/24'], 'dmr.ip-lists.ipsc.block': ['nope'],
      'dmr.ip-lists.openbridge.mode': 'allow', 'dmr.ip-lists.openbridge.allow': ['x'],
    };
    expect(tabCheckErrors(t, reader(values))).toEqual({
      'dmr.ip-lists.mmdvm.block': '10.1.2.3/8 has bits set after its prefix; use 10.0.0.0/8',
      'dmr.ip-lists.openbridge.allow': 'x is not an IP address or CIDR range',
    });
    expect(tabCheckErrors(tab('general'), reader({}))).toEqual({});
  });

  it('warns about an empty allow list, without blocking the save', () => {
    const allow = field('dmr.ip-lists.openbridge.allow');
    expect(allow.warn?.(reader({ 'dmr.ip-lists.openbridge.allow': [] }))).toBe('With no address listed, every peer is refused.');
    expect(allow.warn?.(reader({ 'dmr.ip-lists.openbridge.allow': ['192.0.2.1'] }))).toBeUndefined();
    expect(allow.check?.([])).toBeUndefined();
    expect(field('dmr.ip-lists.mmdvm.allow').warn?.(reader({}))).toBe('With no address listed, every hotspot and repeater is refused.');
  });
});

describe('setup menu routing', () => {
  const reader = (v: Record<string, unknown>) => (p: string) => v[p];

  it('has the routing settings and no explainer text', () => {
    expect(tabPaths(tab('routing'))).toEqual([
      'dmr.routing.disable-dynamic-links', 'dmr.routing.dynamic-timeout', 'dmr.routing.transmit', 'dmr.routing.max-transmission',
      'dmr.routing.contention', 'dmr.routing.auto-create-talkgroups', 'dmr.routing.ephemeral-talkgroups', 'dmr.routing.ephemeral-ttl',
      'dmr.repeater-quota',
    ]);
    expect(tab('routing').intro).toEqual([]);
    expect(tab('routing').group).toBe('DMR');
  });

  it('shows the timeout only while dynamic links are on', () => {
    const f = field('dmr.routing.dynamic-timeout');
    expect(f.show?.(reader({}))).toBe(true);
    expect(f.show?.(reader({ 'dmr.routing.disable-dynamic-links': true }))).toBe(false);
    expect(field('dmr.routing.disable-dynamic-links').invert).toBe(true);
  });

  it('shows ephemeral only for made talkgroups, and the lifetime only when ephemeral', () => {
    const eph = field('dmr.routing.ephemeral-talkgroups');
    const ttl = field('dmr.routing.ephemeral-ttl');
    expect(eph.show?.(reader({}))).toBe(false);
    expect(eph.show?.(reader({ 'dmr.routing.auto-create-talkgroups': true }))).toBe(true);
    expect(ttl.show?.(reader({ 'dmr.routing.auto-create-talkgroups': true }))).toBe(false);
    expect(ttl.show?.(reader({ 'dmr.routing.auto-create-talkgroups': true, 'dmr.routing.ephemeral-talkgroups': true }))).toBe(true);
  });

  it('converts the durations of the API', () => {
    expect(inputValue(field('dmr.routing.dynamic-timeout'), 15 * 6e10)).toBe('15');
    expect(fromInput(field('dmr.routing.dynamic-timeout'), '30')).toBe(30 * 6e10);
    expect(inputValue(field('dmr.routing.ephemeral-ttl'), 24 * 3.6e12)).toBe('24');
    expect(fromInput(field('dmr.routing.ephemeral-ttl'), '1.5')).toBe(1.5 * 3.6e12);
    expect(fromInput(field('dmr.routing.max-transmission'), '180')).toBe(180e9);
  });

  it('turns the longest transmission on at 3 minutes', () => {
    expect(field('dmr.routing.max-transmission').toggle).toEqual({ label: 'Limit transmission length', value: 180e9 });
  });
});
