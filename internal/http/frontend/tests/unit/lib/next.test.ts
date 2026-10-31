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
import { safeNext } from '@/lib/next';

describe('safeNext', () => {
  it('keeps a path on this site', () => {
    expect(safeNext('/oauth/authorize?client_id=https%3A%2F%2Fapp.example')).toBe(
      '/oauth/authorize?client_id=https%3A%2F%2Fapp.example',
    );
  });

  it('sends anything else to Last Heard', () => {
    for (const next of [undefined, '', 'https://evil.example', '//evil.example', '/\\evil.example', 'javascript:alert(1)', ['/a', '/b']]) {
      expect(safeNext(next)).toBe('/lastheard');
    }
  });
});
