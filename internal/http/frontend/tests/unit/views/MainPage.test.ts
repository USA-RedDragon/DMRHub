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
import { mount } from '@vue/test-utils';
import MainPage from '@/views/MainPage.vue';

describe('MainPage', () => {
  it('names the MMDVM network protocol', () => {
    const text = mount(MainPage).text();
    expect(text).toContain('MMDVM network protocol');
    expect(text).not.toMatch(/homebrew|prococol/i);
  });

  it('says the parrot is a plugin that answers only when installed', () => {
    const text = mount(MainPage).text();
    expect(text).toContain('When the admin installed the parrot plugin, it answers on 9990.');
    expect(text).toContain('Talkgroup 4000 unlinks');
    expect(text).not.toContain('DMRHub implements a Parrot');
  });

  it('links the admin guide where it is in the repository', () => {
    const hrefs = mount(MainPage).findAll('a').map((a) => a.attributes('href'));
    expect(hrefs).toContain('https://github.com/USA-RedDragon/DMRHub/blob/main/doc/admin/overview.md');
    expect(hrefs.filter((h) => h?.includes('Admin-Guide.md'))).toEqual([]);
  });
});
