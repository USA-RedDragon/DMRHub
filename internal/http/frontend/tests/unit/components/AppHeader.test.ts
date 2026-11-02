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
import { describe, it, expect, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(() => Promise.resolve({ data: 'DMRHub' })),
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import AppHeader from '@/components/AppHeader.vue';
import source from '@/components/AppHeader.vue?raw';
import { routes, setupwizardRoutes } from '@/router/routes';
import { useUserStore } from '@/store';

describe('AppHeader', () => {
  it('links only to pages that exist', () => {
    const router = createRouter({ history: createMemoryHistory(), routes: routes() });
    const links = [...source.matchAll(/\bto="([^"]+)"/g)].map((m) => m[1]!);
    expect(links.length).toBeGreaterThan(10);
    const missing = links.filter((to) => router.resolve(to).matched.length === 0);
    expect(missing).toEqual([]);
  });

  it('reaches the docs at / only with a full page load', () => {
    expect(source).not.toMatch(/\bto="\/"/);
    expect(source).not.toMatch(/>\s*Home\s*</);
    const plain = [...source.matchAll(/<a\s[^>]*href="\/"[^>]*>\s*([\s\S]*?)\s*<\/a>/g)].map((m) => m[1]!.replace(/<[^>]+>/g, '').trim());
    expect(plain).toEqual(['{{ title }}', 'Docs', '{{ title }}', 'Docs']);
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

  it('shows Invites only while registration is by invite, and Register only while it is not closed', () => {
    const invites = [...source.matchAll(/to="\/admin\/invites"/g)].map((m) => m.index!);
    expect(invites).toHaveLength(2);
    for (const i of invites) {
      expect(source.slice(i - 160, i)).toContain("settingsStore.registration === 'invite'");
    }
    const register = [...source.matchAll(/to="\/register"/g)].map((m) => m.index!);
    expect(register).toHaveLength(2);
    for (const i of register) {
      expect(source.slice(i - 80, i)).toContain("settingsStore.registration !== 'closed'");
    }
    expect([...source.matchAll(/to="\/admin\/message-access"/g)]).toHaveLength(2);
  });

  it('shows Unassigned only while a repeater has no owner', () => {
    const at = [...source.matchAll(/to="\/admin\/repeaters\/unassigned"/g)].map((m) => m.index!);
    expect(at).toHaveLength(2);
    for (const i of at) {
      expect(source.slice(i - 160, i)).toContain('v-if="adminStore.unassignedRepeaters > 0"');
    }
  });

  it('shows only the name of the network on the pages of the setup wizard', async () => {
    for (const path of ['/setup', '/setup/user']) {
      const router = createRouter({ history: createMemoryHistory(), routes: setupwizardRoutes() });
      await router.push(path);
      await router.isReady();
      const wrapper = mount(AppHeader, { global: { plugins: [router, createPinia()] } });
      await flushPromises();
      expect(wrapper.text().trim()).toBe('DMRHub');
      expect(wrapper.findAll('nav')).toHaveLength(0);
      expect(wrapper.find('button').exists()).toBe(false);
      wrapper.unmount();
    }
  });

  it('marks Repeaters on the page of a local group', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: routes() });
    await router.push('/localgroups/5');
    await router.isReady();
    const pinia = createPinia();
    setActivePinia(pinia);
    useUserStore().loggedIn = true;
    const wrapper = mount(AppHeader, { global: { plugins: [router, pinia] } });
    await flushPromises();
    const links = wrapper.findAll('a').filter((a) => a.text().trim() === 'Repeaters');
    expect(links.length).toBeGreaterThan(0);
    for (const a of links) {
      expect(a.classes()).toContain('active');
    }
    wrapper.unmount();
  });
});
