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
import { createMemoryHistory, createRouter } from 'vue-router';

import source from '@/components/AppHeader.vue?raw';
import { routes } from '@/router/routes';

describe('AppHeader', () => {
  it('links only to pages that exist', () => {
    const router = createRouter({ history: createMemoryHistory(), routes: routes() });
    const links = [...source.matchAll(/\bto="([^"]+)"/g)].map((m) => m[1]!);
    expect(links.length).toBeGreaterThan(10);
    const missing = links.filter((to) => router.resolve(to).matched.length === 0);
    expect(missing).toEqual([]);
  });

  it('opens the Admin menu without going anywhere', () => {
    const triggers = [...source.matchAll(/<DropdownMenuTrigger[\s\S]*?<\/DropdownMenuTrigger>/g)].map((m) => m[0]);
    const admin = triggers.find((t) => /\bAdmin\b/.test(t));
    expect(admin).toBeDefined();
    expect(admin).not.toMatch(/router-link|\bto=|href=/);
    expect(admin).toMatch(/<button[^>]*type="button"/);
  });

  it('shows RadioID Drift only with radio ID validation on', () => {
    const at = [...source.matchAll(/to="\/admin\/users\/radio-id-drift"/g)].map((m) => m.index!);
    expect(at).toHaveLength(2);
    for (const i of at) {
      expect(source.slice(i - 160, i)).toContain('v-if="settingsStore.radioIDValidation"');
    }
  });
});
