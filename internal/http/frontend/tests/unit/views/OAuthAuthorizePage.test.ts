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
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

const query = {
  response_type: 'code',
  client_id: 'https://app.example',
  redirect_uri: 'https://app.example/callback',
  scope: 'talkgroups:read messages:read',
  state: 'xyz',
  code_challenge: 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM',
  code_challenge_method: 'S256',
};
const replace = vi.fn();

vi.mock('vue-router', () => ({
  useRoute: () => ({ query, fullPath: '/oauth/authorize?client_id=https%3A%2F%2Fapp.example' }),
  useRouter: () => ({ replace }),
}));

vi.mock('@/lib/navigate', () => ({
  goTo: vi.fn(),
}));

import API from '@/services/API';
import { goTo } from '@/lib/navigate';
import OAuthAuthorizePage from '@/views/auth/OAuthAuthorizePage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

const scopes = [
  { name: 'talkgroups:read', label: 'talkgroups', description: 'See the talkgroups of this network' },
  { name: 'messages:read', label: 'messages', description: 'Read your text messages, and get new ones as they arrive' },
];

function serve(me: unknown = { id: 3191868, callsign: 'KI5VMF', username: 'ki5vmf' }) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/users/me') {
      return me ? Promise.resolve({ data: me }) : Promise.reject({ response: { status: 401 } });
    }
    if (url === '/oauth/authorize') {
      return Promise.resolve({ data: { origin: 'https://app.example', scopes } });
    }
    return Promise.reject(new Error(`unexpected GET ${url}`));
  });
}

async function mountPage() {
  const wrapper = mount(OAuthAuthorizePage, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('OAuthAuthorizePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('shows the app, the account and what the app asks for', async () => {
    serve();
    const wrapper = await mountPage();
    expect(mockAPI.get).toHaveBeenCalledWith('/oauth/authorize', { params: query });
    const text = wrapper.text();
    expect(text).toContain('Allow this app to use your DMRHub account?');
    expect(wrapper.find('[data-test="oauth-origin"]').text()).toBe('https://app.example');
    expect(text).toContain('Signed in as KI5VMF (3191868)');
    expect(wrapper.findAll('[data-test="oauth-scope"]').map((li) => li.text())).toEqual(scopes.map((s) => s.description));
    expect(text).toContain('It cannot change your password or account, and it never gets admin access.');
    expect(text).not.toMatch(/talkgroups:read|messages:read/);
  });

  it('sends a visitor who is not logged in to log in and back', async () => {
    serve(null);
    await mountPage();
    expect(replace).toHaveBeenCalledWith({
      path: '/login',
      query: { next: '/oauth/authorize?client_id=https%3A%2F%2Fapp.example' },
    });
  });

  it('allows the app and takes the browser back to it', async () => {
    serve();
    mockAPI.post.mockResolvedValue({ data: { redirect: 'https://app.example/callback?code=abc&state=xyz' } });
    const wrapper = await mountPage();
    await wrapper.find('[data-test="oauth-allow"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/oauth/authorize', { ...query, approve: true });
    expect(goTo).toHaveBeenCalledWith('https://app.example/callback?code=abc&state=xyz');
    expect(wrapper.text()).toContain('Taking you back to https://app.example.');
  });

  it('tells the app when the user does not allow it', async () => {
    serve();
    mockAPI.post.mockResolvedValue({ data: { redirect: 'https://app.example/callback?error=access_denied&state=xyz' } });
    const wrapper = await mountPage();
    await wrapper.find('[data-test="oauth-deny"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/oauth/authorize', { ...query, approve: false });
    expect(goTo).toHaveBeenCalledWith('https://app.example/callback?error=access_denied&state=xyz');
    expect(wrapper.text()).toContain('You did not allow access. Taking you back to https://app.example.');
  });

  it('explains a request that is not valid and never redirects to an unchecked place', async () => {
    mockAPI.get.mockImplementation((url: string) => {
      if (url === '/users/me') {
        return Promise.resolve({ data: { id: 3191868, callsign: 'KI5VMF' } });
      }
      return Promise.reject({ response: { status: 400, data: { error: 'invalid_client' } } });
    });
    const wrapper = await mountPage();
    expect(wrapper.text()).toContain('This app sent a request that DMRHub cannot accept.');
    expect(wrapper.find('[data-test="oauth-allow"]').exists()).toBe(false);
    expect(goTo).not.toHaveBeenCalled();
  });
});
