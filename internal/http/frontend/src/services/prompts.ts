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

/** A voice set of the spoken prompts. */
export type PromptVoice = { id: string; name: string; about: string };

/** A clip of the voice sets and its words. */
export type PromptClip = { name: string; text: string };

/** The voice sets, the clips, and the voice of the hub for users who did not pick one. */
export type PromptList = { voices: PromptVoice[]; clips: PromptClip[]; default: string };

export async function getPrompts(): Promise<PromptList> {
  const res = await API.get('/prompts');
  return res.data;
}

/** The URL of the preview of a clip in a voice, for the users' Settings. */
export function previewURL(voice: string, clip: string): string {
  return `/api/v1/prompts/${encodeURIComponent(voice)}/${encodeURIComponent(clip)}.wav`;
}

/** The URL of the preview of a clip for the Setup page, in voice or the default voice of the hub. */
export function setupPreviewURL(clip: string, voice?: string): string {
  const q = voice ? `?voice=${encodeURIComponent(voice)}` : '';
  return `/api/v1/config/prompts/${encodeURIComponent(clip)}.wav${q}`;
}

/** Sets the voice of the user's prompts, or the default voice of the hub with ''. */
export async function setPromptVoice(voice: string) {
  const res = await API.put('/users/me/settings/prompt-voice', { voice });
  return res.data;
}
