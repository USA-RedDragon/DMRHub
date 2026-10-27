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
  TABS, defaultNote, keepEdits, sourceChips, tabDirty, tabFields, tabOfHash, tabPaths, tabPayload, tabReadOnly,
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
      'general', 'http', 'database', 'redis', 'smtp', 'metrics', 'dmr', 'ipsc', 'openbridge', 'webtransport', 'modem',
      'transcoding', 'positions', 'messages', 'plugins', 'pcroute',
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
