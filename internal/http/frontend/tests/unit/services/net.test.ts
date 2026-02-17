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

import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest';
import {
  getNets,
  getNet,
  startNet,
  stopNet,
  patchNet,
  getNetCheckIns,
  exportNetCheckIns,
  getScheduledNets,
  getScheduledNet,
  createScheduledNet,
  updateScheduledNet,
  deleteScheduledNet,
} from '@/services/net';

// Mock the API module
vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
const mockAPI = API as unknown as {
  get: Mock;
  post: Mock;
  patch: Mock;
  delete: Mock;
};

describe('net service', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('getNets', () => {
    it('calls GET /nets with no params', async () => {
      mockAPI.get.mockResolvedValue({ data: { nets: [], total: 0 } });
      await getNets();
      expect(mockAPI.get).toHaveBeenCalledWith('/nets');
    });

    it('calls GET /nets with page and limit', async () => {
      mockAPI.get.mockResolvedValue({ data: { nets: [], total: 0 } });
      await getNets({ page: 2, limit: 10 });
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('page=2'));
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('limit=10'));
    });

    it('calls GET /nets with talkgroup_id filter', async () => {
      mockAPI.get.mockResolvedValue({ data: { nets: [], total: 0 } });
      await getNets({ talkgroup_id: 1 });
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('talkgroup_id=1'));
    });

    it('calls GET /nets with active filter', async () => {
      mockAPI.get.mockResolvedValue({ data: { nets: [], total: 0 } });
      await getNets({ active: true });
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('active=true'));
    });

    it('calls GET /nets with showcase filter', async () => {
      mockAPI.get.mockResolvedValue({ data: { nets: [], total: 0 } });
      await getNets({ showcase: true });
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('showcase=true'));
    });
  });

  describe('getNet', () => {
    it('calls GET /nets/:id', async () => {
      mockAPI.get.mockResolvedValue({ data: { id: 42 } });
      await getNet(42);
      expect(mockAPI.get).toHaveBeenCalledWith('/nets/42');
    });
  });

  describe('startNet', () => {
    it('calls POST /nets/start with body', async () => {
      const body = { talkgroup_id: 1, description: 'Test net' };
      mockAPI.post.mockResolvedValue({ data: { id: 1 } });
      await startNet(body);
      expect(mockAPI.post).toHaveBeenCalledWith('/nets/start', body);
    });

    it('includes duration_minutes when provided', async () => {
      const body = { talkgroup_id: 1, duration_minutes: 60 };
      mockAPI.post.mockResolvedValue({ data: { id: 1 } });
      await startNet(body);
      expect(mockAPI.post).toHaveBeenCalledWith('/nets/start', body);
    });
  });

  describe('stopNet', () => {
    it('calls POST /nets/:id/stop', async () => {
      mockAPI.post.mockResolvedValue({ data: { id: 5 } });
      await stopNet(5);
      expect(mockAPI.post).toHaveBeenCalledWith('/nets/5/stop');
    });
  });

  describe('patchNet', () => {
    it('calls PATCH /nets/:id with showcase body', async () => {
      mockAPI.patch.mockResolvedValue({ data: { id: 3, showcase: true } });
      await patchNet(3, { showcase: true });
      expect(mockAPI.patch).toHaveBeenCalledWith('/nets/3', { showcase: true });
    });

    it('calls PATCH /nets/:id to disable showcase', async () => {
      mockAPI.patch.mockResolvedValue({ data: { id: 3, showcase: false } });
      await patchNet(3, { showcase: false });
      expect(mockAPI.patch).toHaveBeenCalledWith('/nets/3', { showcase: false });
    });
  });

  describe('getNetCheckIns', () => {
    it('calls GET /nets/:id/checkins', async () => {
      mockAPI.get.mockResolvedValue({ data: { check_ins: [], total: 0 } });
      await getNetCheckIns(10);
      expect(mockAPI.get).toHaveBeenCalledWith('/nets/10/checkins');
    });

    it('includes pagination params', async () => {
      mockAPI.get.mockResolvedValue({ data: { check_ins: [], total: 0 } });
      await getNetCheckIns(10, { page: 2, limit: 25 });
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('page=2'));
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('limit=25'));
    });
  });

  describe('exportNetCheckIns', () => {
    it('calls GET /nets/:id/checkins/export with csv format', async () => {
      mockAPI.get.mockResolvedValue({ data: 'csv-data' });
      await exportNetCheckIns(7, 'csv');
      expect(mockAPI.get).toHaveBeenCalledWith('/nets/7/checkins/export?format=csv', {
        responseType: 'blob',
      });
    });

    it('calls GET /nets/:id/checkins/export with json format', async () => {
      mockAPI.get.mockResolvedValue({ data: {} });
      await exportNetCheckIns(7, 'json');
      expect(mockAPI.get).toHaveBeenCalledWith('/nets/7/checkins/export?format=json', {
        responseType: 'json',
      });
    });
  });

  describe('getScheduledNets', () => {
    it('calls GET /nets/scheduled with no params', async () => {
      mockAPI.get.mockResolvedValue({ data: { scheduled_nets: [], total: 0 } });
      await getScheduledNets();
      expect(mockAPI.get).toHaveBeenCalledWith('/nets/scheduled');
    });

    it('includes talkgroup_id param', async () => {
      mockAPI.get.mockResolvedValue({ data: { scheduled_nets: [], total: 0 } });
      await getScheduledNets({ talkgroup_id: 5 });
      expect(mockAPI.get).toHaveBeenCalledWith(expect.stringContaining('talkgroup_id=5'));
    });
  });

  describe('getScheduledNet', () => {
    it('calls GET /nets/scheduled/:id', async () => {
      mockAPI.get.mockResolvedValue({ data: { id: 3 } });
      await getScheduledNet(3);
      expect(mockAPI.get).toHaveBeenCalledWith('/nets/scheduled/3');
    });
  });

  describe('createScheduledNet', () => {
    it('calls POST /nets/scheduled with body', async () => {
      const body = {
        talkgroup_id: 1,
        name: 'Weekly Net',
        day_of_week: 3,
        time_of_day: '19:00',
        timezone: 'America/Chicago',
      };
      mockAPI.post.mockResolvedValue({ data: { id: 1 } });
      await createScheduledNet(body);
      expect(mockAPI.post).toHaveBeenCalledWith('/nets/scheduled', body);
    });
  });

  describe('updateScheduledNet', () => {
    it('calls PATCH /nets/scheduled/:id with body', async () => {
      const body = { name: 'Updated Net', enabled: false };
      mockAPI.patch.mockResolvedValue({ data: { id: 2 } });
      await updateScheduledNet(2, body);
      expect(mockAPI.patch).toHaveBeenCalledWith('/nets/scheduled/2', body);
    });
  });

  describe('deleteScheduledNet', () => {
    it('calls DELETE /nets/scheduled/:id', async () => {
      mockAPI.delete.mockResolvedValue({ data: {} });
      await deleteScheduledNet(8);
      expect(mockAPI.delete).toHaveBeenCalledWith('/nets/scheduled/8');
    });
  });
});
