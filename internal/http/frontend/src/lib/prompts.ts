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

import type { Option, Read } from '@/lib/setupMenu';

/** The voice sets that DMRHub embeds, as internal/prompts/voices holds them. */
export const PROMPT_VOICES: Option[] = [
  { value: 'kitten-mini-luna', label: 'Luna' },
  { value: 'kitten-mini-bruno', label: 'Bruno' },
  { value: 'kitten-mini-hugo', label: 'Hugo' },
  { value: 'kokoro-bm-lewis', label: 'Lewis' },
];

/** A refusal or on-air code reply that the hub speaks, with when it plays. */
export interface SpokenRefusal {
  clip: string;
  when: string;
  text: string;
  /** Reports whether the settings that read gives make the hub play it, when not always. */
  applies?: (read: Read) => boolean;
}

/** The refusals and on-air code replies that the hub speaks, with when each plays. */
export const SPOKEN_REFUSALS: SpokenRefusal[] = [
  { clip: 'talkgroup-not-found', when: 'The talkgroup does not exist', text: 'Talkgroup not found.' },
  { clip: 'talkgroup-restricted', when: 'The talkgroup is not open to the repeater', text: 'Talkgroup restricted.' },
  { clip: 'talkgroup-blocked', when: 'The repeater blocks the talkgroup', text: 'Talkgroup blocked here.' },
  { clip: 'timeslot-reserved', when: 'The timeslot is locked to other users', text: 'Timeslot reserved.' },
  { clip: 'radio-id-not-registered', when: 'The radio may not transmit on this network', text: 'Radio ID not registered.' },
  { clip: 'time-out', when: 'The transmission ran past the longest one allowed', text: 'Time out.' },
  {
    clip: 'channel-busy',
    when: 'Another call held the timeslot when the radio keyed up',
    text: 'Channel busy.',
    applies: (read) => read('dmr.routing.contention') === 'first',
  },
  { clip: 'code-needed', when: 'The radio needs its on-air code', text: 'Code needed.' },
  { clip: 'unlocked', when: 'The on-air code was right', text: 'Unlocked.' },
  { clip: 'wrong-code', when: 'The on-air code was wrong', text: 'Wrong code.' },
];

let playing: HTMLAudioElement | null = null;

/** Plays the WAV file at url, after it stops any prompt that plays. */
export function playPrompt(url: string): Promise<void> {
  playing?.pause();
  playing = new Audio(url);
  return playing.play();
}
