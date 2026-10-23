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
import { h } from 'vue';
import type { ColumnDef } from '@tanstack/vue-table';
import { DataTable } from '@/components/ui/data-table';

type Row = Record<string, unknown>;

const columns: ColumnDef<Row, unknown>[] = [
  { id: 'input', header: 'Name', cell: () => h('input', { 'data-test': 'cell-input' }) },
  { id: 'link', header: 'Link', cell: () => h('a', { href: '#x', 'data-test': 'cell-link' }, 'x') },
];

function key(el: Element, k: string) {
  const event = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true });
  el.dispatchEvent(event);
  return event;
}

describe('DataTable rows', () => {
  it('leaves keys alone in a table whose rows are not clickable', () => {
    const wrapper = mount(DataTable, { attachTo: document.body, props: { columns, data: [{ id: 1 }] } });
    const input = wrapper.find('[data-test="cell-input"]').element;
    expect(key(input, ' ').defaultPrevented).toBe(false);
    expect(key(input, 'Enter').defaultPrevented).toBe(false);
    wrapper.unmount();
  });

  it('leaves keys in a cell alone in a table whose rows are clickable', () => {
    const wrapper = mount(DataTable, {
      attachTo: document.body,
      props: { columns, data: [{ id: 1 }], rowClickable: true },
    });
    const input = wrapper.find('[data-test="cell-input"]').element;
    expect(key(input, ' ').defaultPrevented).toBe(false);
    expect(wrapper.emitted('rowClick')).toBeUndefined();
    wrapper.unmount();
  });

  it('selects a clickable row with Enter, Space or a click, but not with a click on a link', async () => {
    const wrapper = mount(DataTable, {
      attachTo: document.body,
      props: { columns, data: [{ id: 1 }], rowClickable: true },
    });
    const row = wrapper.find('tbody tr');
    expect(key(row.element, 'Enter').defaultPrevented).toBe(true);
    expect(key(row.element, ' ').defaultPrevented).toBe(true);
    await wrapper.find('[data-test="cell-link"]').trigger('click');
    expect(wrapper.emitted('rowClick')).toHaveLength(2);
    await row.find('td').trigger('click');
    expect(wrapper.emitted('rowClick')).toHaveLength(3);
    wrapper.unmount();
  });
});
