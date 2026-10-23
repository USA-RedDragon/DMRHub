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

import {
  aprsName,
  formatAgo,
  formatCoordinates,
  formatMotion,
  formatRetention,
  isPositionPush,
  serviceSymbol,
  summary,
  symbolLabel,
  trackURL,
  type Position,
} from '@/services/positions';

const pos = (over: Partial<Position> = {}): Position => ({
  radio_id: 3191868,
  shared: false,
  latitude: 30.26717,
  longitude: -97.74317,
  format: 'lrrp',
  accuracy_m: 10,
  speed_mps: null,
  heading_deg: null,
  altitude_m: null,
  fix_time: null,
  received_at: '2026-10-23T20:00:00Z',
  repeater_id: 319186801,
  slot: 2,
  service_id: 5057,
  ...over,
});

describe('positions service', () => {
  it('formats coordinates with a real minus sign', () => {
    expect(formatCoordinates(pos())).toBe('30.2672, −97.7432');
    expect(formatCoordinates(pos({ latitude: -33.86883, longitude: 151.2093 }))).toBe('−33.8688, 151.2093');
  });

  it('formats speed in km/h with a compass direction', () => {
    expect(formatMotion(pos())).toBe('');
    expect(formatMotion(pos({ speed_mps: 4, heading_deg: 45 }))).toBe('14 km/h NE');
    expect(formatMotion(pos({ speed_mps: 4, heading_deg: 350 }))).toBe('14 km/h N');
    expect(formatMotion(pos({ speed_mps: 4 }))).toBe('14 km/h');
    expect(formatMotion(pos({ speed_mps: 0, heading_deg: 90 }))).toBe('0 km/h');
  });

  it('says how long ago', () => {
    const now = Date.parse('2026-10-23T20:00:00Z');
    expect(formatAgo('2026-10-23T19:59:30Z', now)).toBe('just now');
    expect(formatAgo('2026-10-23T19:56:00Z', now)).toBe('4 min ago');
    expect(formatAgo('2026-10-23T19:00:00Z', now)).toBe('1 hour ago');
    expect(formatAgo('2026-10-23T15:00:00Z', now)).toBe('5 hours ago');
    expect(formatAgo('2026-10-20T20:00:00Z', now)).toBe('3 days ago');
  });

  it('names retention, symbols and APRS-IS callsigns', () => {
    expect(formatRetention(24)).toBe('24 hours');
    expect(formatRetention(168)).toBe('7 days');
    expect(formatRetention(2160)).toBe('90 days');
    expect(serviceSymbol(5050)).toBe('house');
    expect(serviceSymbol(5057)).toBe('person');
    expect(serviceSymbol(5059)).toBe('car');
    expect(serviceSymbol(5060)).toBeUndefined();
    expect(symbolLabel('pickup')).toBe('Pickup');
    expect(aprsName('KI5VMF', '9')).toBe('KI5VMF-9');
    expect(aprsName('KI5VMF', '')).toBe('KI5VMF');
    expect(trackURL(3191868, 'gpx')).toBe('/api/v1/positions/3191868/track?format=gpx');
  });

  it('sums up a position for the list', () => {
    expect(summary(pos({ speed_mps: 4, heading_deg: 45 }))).toBe('30.2672, −97.7432 · LRRP · 14 km/h NE');
    expect(summary(pos({ format: 'nmea' }))).toBe('30.2672, −97.7432 · NMEA');
  });

  it('tells position frames from message frames', () => {
    expect(isPositionPush({ kind: 'position', position: pos() })).toBe(true);
    expect(isPositionPush({ boxes: ['inbox'], message: {} })).toBe(false);
    expect(isPositionPush({ kind: 'position' })).toBe(false);
    expect(isPositionPush(null)).toBe(false);
  });
});
