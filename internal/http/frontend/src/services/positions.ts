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
import API from '@/services/API';

export type PositionFormat = 'lrrp' | 'nmea' | 'hdap';

export type Position = {
  radio_id: number;
  callsign?: string;
  shared: boolean;
  latitude: number;
  longitude: number;
  format: PositionFormat;
  accuracy_m: number | null;
  speed_mps: number | null;
  heading_deg: number | null;
  altitude_m: number | null;
  fix_time: string | null;
  received_at: string;
  repeater_id: number;
  slot: number;
  service_id: number;
};

export type RadioPositions = {
  latest: Position | null;
  track: Position[];
};

export type PositionRadio = {
  radio_id: number;
  tracking: boolean;
  interval_minutes: number;
  retention_hours: number;
  aprs: boolean;
  aprs_ssid: string;
  aprs_symbol: string;
  aprs_comment: string;
};

export type PositionRadios = {
  radios: PositionRadio[];
  intervals: number[];
  retention_hours: number[];
  aprs: boolean;
  callsign: string;
};

export type PositionRadioPut = Omit<PositionRadio, 'radio_id'>;

export type PositionPush = {
  kind: 'position';
  position: Position;
};

export const WINDOWS = [
  { hours: 24, label: '24 hours' },
  { hours: 168, label: '7 days' },
  { hours: 720, label: '30 days' },
] as const;

export const SYMBOLS = [
  { name: 'house', label: 'House' },
  { name: 'train', label: 'Train' },
  { name: 'tractor', label: 'Tractor' },
  { name: 'pickup', label: 'Pickup' },
  { name: 'van', label: 'Van' },
  { name: 'phone', label: 'Phone' },
  { name: 'camp', label: 'Camp' },
  { name: 'person', label: 'Person' },
  { name: 'bike', label: 'Bike' },
  { name: 'car', label: 'Car' },
] as const;

export const SSIDS = [
  { value: '7', label: '-7 handheld' },
  { value: '9', label: '-9 mobile' },
  { value: '5', label: '-5 other network' },
  { value: '10', label: '-10 internet' },
  { value: '', label: 'none' },
] as const;

const FIRST_SERVICE = 5050;

export async function getPositions(hours: number): Promise<Position[]> {
  const res = await API.get('/positions', { params: { hours } });
  return res.data.positions ?? [];
}

export async function getRadioPositions(radio: number, hours: number): Promise<RadioPositions> {
  const res = await API.get(`/positions/${radio}`, { params: { hours } });
  return { latest: res.data.latest ?? null, track: res.data.track ?? [] };
}

export async function getPositionRadios(): Promise<PositionRadios> {
  const res = await API.get('/positions/radios');
  return res.data;
}

export async function setPositionRadio(radio: number, body: PositionRadioPut): Promise<PositionRadio> {
  const res = await API.put(`/positions/${radio}/settings`, body);
  return res.data;
}

export function trackURL(radio: number, format: 'gpx' | 'geojson' | 'csv'): string {
  return `/api/v1/positions/${radio}/track?format=${format}`;
}

/** isPositionPush reports whether a frame of /ws/messages is a new position. */
export function isPositionPush(frame: unknown): frame is PositionPush {
  const f = frame as Partial<PositionPush> | null;
  return !!f && f.kind === 'position' && !!f.position && typeof f.position.radio_id === 'number';
}

/** serviceSymbol is the symbol of the location service a radio reports to. */
export function serviceSymbol(service: number): string | undefined {
  return SYMBOLS[service - FIRST_SERVICE]?.name;
}

export function symbolLabel(name: string): string {
  return SYMBOLS.find((s) => s.name === name)?.label ?? name;
}

function signed(v: number): string {
  return (v < 0 ? '−' : '') + Math.abs(v).toFixed(4);
}

export function formatCoordinates(p: Pick<Position, 'latitude' | 'longitude'>): string {
  return `${signed(p.latitude)}, ${signed(p.longitude)}`;
}

const COMPASS = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];

export function formatMotion(p: Pick<Position, 'speed_mps' | 'heading_deg'>): string {
  if (p.speed_mps === null || p.speed_mps === undefined) {
    return '';
  }
  const kmh = `${Math.round(p.speed_mps * 3.6)} km/h`;
  if (p.heading_deg === null || p.heading_deg === undefined || p.speed_mps === 0) {
    return kmh;
  }
  const dir = COMPASS[Math.round((((p.heading_deg % 360) + 360) % 360) / 45) % 8];
  return `${kmh} ${dir}`;
}

export function formatAgo(iso: string, now: number = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (seconds < 60) {
    return 'just now';
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes} min ago`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 48) {
    return hours === 1 ? '1 hour ago' : `${hours} hours ago`;
  }
  return `${Math.round(hours / 24)} days ago`;
}

export function formatRetention(hours: number): string {
  if (hours === 24) {
    return '24 hours';
  }
  return hours % 24 === 0 ? `${hours / 24} days` : `${hours} hours`;
}

export function aprsName(callsign: string, ssid: string): string {
  return ssid ? `${callsign}-${ssid}` : callsign;
}

/** summary is the second line of a radio in the list: where it is and how it moves. */
export function summary(p: Position): string {
  return [formatCoordinates(p), p.format.toUpperCase(), formatMotion(p)].filter((s) => s !== '').join(' · ');
}
