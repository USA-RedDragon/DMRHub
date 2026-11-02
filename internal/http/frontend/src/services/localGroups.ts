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

export type UserRef = { id: number; callsign: string };

/** A repeater in a local group, or asked to join it when state is asked. */
export type LocalGroupRepeater = {
  id: number;
  callsign: string;
  owner: UserRef;
  slot: 1 | 2;
  state: 'member' | 'asked';
  mine: boolean;
};

/** A local group as the viewer sees it. */
export type LocalGroup = {
  id: number;
  name: string;
  talkgroup: number;
  owner: UserRef;
  members: number;
  mine: boolean;
  can_manage: boolean;
  involved: boolean;
  repeaters: LocalGroupRepeater[];
};

export type LocalGroupList = { groups: LocalGroup[]; talkgroups: number[] };

/** A local group that a repeater is in, or asked to join. */
export type RepeaterLocalGroup = {
  id: number;
  name: string;
  talkgroup: number;
  slot: 1 | 2;
  state: 'member' | 'asked';
  owner: UserRef;
  asked_by: UserRef;
  repeaters: number;
};

export const listLocalGroups = async (): Promise<LocalGroupList> => (await API.get('/localgroups')).data;

export const getLocalGroup = async (id: number): Promise<LocalGroup> => (await API.get(`/localgroups/${id}`)).data;

export const createLocalGroup = async (name: string, talkgroup: number): Promise<LocalGroup> =>
  (await API.post('/localgroups', { name, talkgroup })).data;

export const saveLocalGroup = async (id: number, name: string, talkgroup: number): Promise<LocalGroup> =>
  (await API.patch(`/localgroups/${id}`, { name, talkgroup })).data;

export const deleteLocalGroup = async (id: number): Promise<void> => {
  await API.delete(`/localgroups/${id}`);
};

export const addLocalGroupRepeater = async (id: number, repeater: number, slot: 1 | 2): Promise<LocalGroup> =>
  (await API.post(`/localgroups/${id}/repeaters`, { repeater_id: repeater, slot })).data;

export const setLocalGroupSlot = async (id: number, repeater: number, slot: 1 | 2): Promise<LocalGroup> =>
  (await API.patch(`/localgroups/${id}/repeaters/${repeater}`, { slot })).data;

export const removeLocalGroupRepeater = async (id: number, repeater: number): Promise<void> => {
  await API.delete(`/localgroups/${id}/repeaters/${repeater}`);
};

export const acceptLocalGroup = async (id: number, repeater: number): Promise<LocalGroup> =>
  (await API.post(`/localgroups/${id}/repeaters/${repeater}/accept`)).data;

export const repeaterLocalGroups = async (repeater: number): Promise<RepeaterLocalGroup[]> => {
  const data = (await API.get(`/repeaters/${repeater}/localgroups`)).data;
  return Array.isArray(data) ? data : [];
};
