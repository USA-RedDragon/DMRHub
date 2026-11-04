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

import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import PromptVoiceCard from '@/components/users/PromptVoiceCard.vue';
import PromptList from '@/components/setup/menu/PromptList.vue';
import { SPOKEN_REFUSALS } from '@/lib/prompts';

const mockAPI = API as unknown as { get: Mock; put: Mock };
const played: string[] = [];

class FakeAudio {
  constructor(public src: string) {
    played.push(src);
  }
  play() {
    return Promise.resolve();
  }
  pause() {}
}

const list = {
  voices: [
    { id: 'kitten-mini-luna', name: 'Luna', about: '' },
    { id: 'kokoro-bm-lewis', name: 'Lewis', about: '' },
  ],
  clips: [],
  default: 'kitten-mini-luna',
};

beforeEach(() => {
  mockAPI.get.mockReset();
  mockAPI.put.mockReset();
  played.length = 0;
  vi.stubGlobal('Audio', FakeAudio);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('PromptVoiceCard', () => {
  it('lists the voices with the network default first and saves a pick', async () => {
    mockAPI.get.mockResolvedValue({ data: list });
    mockAPI.put.mockResolvedValue({ data: { prompt_voice: 'kokoro-bm-lewis' } });
    const wrapper = mount(PromptVoiceCard, { props: { voice: '', defaultVoice: 'kitten-mini-luna' } });
    await flushPromises();
    expect(mockAPI.get).toHaveBeenCalledWith('/prompts');
    const options = wrapper.findAll('[data-test="prompt-voice"] option').map((o) => o.text());
    expect(options).toEqual(['Network default (Luna)', 'Luna', 'Lewis']);

    await wrapper.find('[data-test="prompt-voice"]').setValue('kokoro-bm-lewis');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/prompt-voice', { voice: 'kokoro-bm-lewis' });

    await wrapper.find('[data-test="prompt-voice-play"]').trigger('click');
    await flushPromises();
    expect(played).toEqual(['/api/v1/prompts/kokoro-bm-lewis/talkgroup-not-found.wav']);
  });

  it('plays the network default when the user picked none', async () => {
    mockAPI.get.mockResolvedValue({ data: list });
    const wrapper = mount(PromptVoiceCard, { props: { voice: '', defaultVoice: 'kitten-mini-luna' } });
    await flushPromises();
    await wrapper.find('[data-test="prompt-voice-play"]').trigger('click');
    await flushPromises();
    expect(played).toEqual(['/api/v1/prompts/kitten-mini-luna/talkgroup-not-found.wav']);
  });

  it('shows an error when the save fails', async () => {
    mockAPI.get.mockResolvedValue({ data: list });
    mockAPI.put.mockRejectedValue({ response: { data: { error: 'Send voice as the ID of a voice' } } });
    const wrapper = mount(PromptVoiceCard, { props: { voice: '', defaultVoice: 'kitten-mini-luna' } });
    await flushPromises();
    await wrapper.find('[data-test="prompt-voice"]').setValue('kokoro-bm-lewis');
    await flushPromises();
    expect(wrapper.find('[data-test="prompt-voice-error"]').text()).toBe('Send voice as the ID of a voice');
  });
});

describe('PromptList', () => {
  it('shows nothing while the spoken refusals are off', () => {
    const wrapper = mount(PromptList, { props: { read: () => false } });
    expect(wrapper.find('[data-test="prompt-list"]').exists()).toBe(false);
  });

  it('lists each refusal and plays it in the voice on the page', async () => {
    const values: Record<string, unknown> = { 'dmr.routing.spoken-refusals': true, 'dmr.routing.prompt-voice': 'kitten-mini-hugo' };
    const wrapper = mount(PromptList, { props: { read: (p: string) => values[p] } });
    const rows = wrapper.findAll('tr[data-test^="prompt-"]');
    expect(rows.map((r) => r.attributes('data-test'))).toEqual(
      SPOKEN_REFUSALS.filter((r) => r.clip !== 'channel-busy').map((r) => `prompt-${r.clip}`),
    );
    expect(wrapper.find('[data-test="prompt-wrong-code"]').text()).toContain('"Wrong code."');
    await wrapper.find('[data-test="prompt-wrong-code"] [data-test="prompt-play"]').trigger('click');
    await flushPromises();
    expect(played).toEqual(['/api/v1/config/prompts/wrong-code.wav?voice=kitten-mini-hugo']);
  });

  it('lists Channel busy only when the first call holds the timeslot', () => {
    const values: Record<string, unknown> = { 'dmr.routing.spoken-refusals': true, 'dmr.routing.contention': 'mix' };
    const mix = mount(PromptList, { props: { read: (p: string) => values[p] } });
    expect(mix.find('[data-test="prompt-channel-busy"]').exists()).toBe(false);

    values['dmr.routing.contention'] = 'first';
    const first = mount(PromptList, { props: { read: (p: string) => values[p] } });
    const rows = first.findAll('tr[data-test^="prompt-"]').map((r) => r.attributes('data-test'));
    expect(rows).toEqual(SPOKEN_REFUSALS.map((r) => `prompt-${r.clip}`));
    expect(first.find('[data-test="prompt-channel-busy"]').text()).toContain('"Channel busy."');
  });
});
