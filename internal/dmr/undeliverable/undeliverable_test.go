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

package undeliverable_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"github.com/stretchr/testify/assert"
)

func TestEveryReasonIsAPlainSentence(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		undeliverable.ReasonInvalidDataType: "The burst for IPSC repeater 314926 had an invalid data type.",
		undeliverable.ReasonNoIPSCForm:      "IPSC repeater 314926 cannot carry this kind of burst.",
		undeliverable.ReasonFrameType3:      "The burst for IPSC repeater 314926 had an unknown frame type.",
		undeliverable.ReasonFEC:             "The burst for IPSC repeater 314926 was too damaged to repair.",
		undeliverable.ReasonSlotType:        "The burst could not be rebuilt for IPSC repeater 314926.",
		undeliverable.ReasonStreamID:        "The call ID does not fit the link to IPSC repeater 314926.",
		undeliverable.ReasonBuild:           "The burst could not be built for IPSC repeater 314926.",
		undeliverable.ReasonEncode:          "The burst could not be encoded for IPSC repeater 314926.",
		undeliverable.ReasonDataInVoice:     "A data burst for IPSC repeater 314926 came in the middle of a voice call.",
		undeliverable.ReasonUndecodable:     "The burst for IPSC repeater 314926 could not be read.",
		undeliverable.ReasonSlotBusy:        "IPSC repeater 314926 was busy on that slot.",
	}
	assert.Len(t, undeliverable.Reasons(), len(want))
	for _, reason := range undeliverable.Reasons() {
		assert.Equal(t, want[reason], undeliverable.Sentence("ipsc", 314926, reason), reason)
	}
}

func TestSentenceNamesEachTransport(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "MMDVM repeater 311000 was busy on that slot.", undeliverable.Sentence("mmdvm", 311000, undeliverable.ReasonSlotBusy))
	assert.Equal(t, "OpenBridge peer 311000 was busy on that slot.", undeliverable.Sentence("openbridge", 311000, undeliverable.ReasonSlotBusy))
	assert.Equal(t, "Repeater 42 was busy on that slot.", undeliverable.Sentence("newproto", 42, undeliverable.ReasonSlotBusy))
}

func TestSentenceCopesWithMissingParts(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "An IPSC repeater was busy on that slot.", undeliverable.Sentence("ipsc", 0, undeliverable.ReasonSlotBusy))
	assert.Equal(t, "The burst for an IPSC repeater could not be read.", undeliverable.Sentence("ipsc", 0, undeliverable.ReasonUndecodable))
	assert.Equal(t, "The burst could not be prepared for IPSC repeater 314926.", undeliverable.Sentence("ipsc", 314926, "new_reason"))
	assert.Equal(t, "The burst could not be prepared for a repeater.", undeliverable.Sentence("", 0, ""))
}

func TestFromOldForm(t *testing.T) {
	t.Parallel()
	for old, want := range map[string]string{
		"ipsc:314926:slot_busy":            "IPSC repeater 314926 was busy on that slot.",
		"ipsc:311304200:no_ipsc_form":      "IPSC repeater 311304200 cannot carry this kind of burst.",
		"ipsc:311304200":                   "The burst could not be prepared for IPSC repeater 311304200.",
		"ipsc::no_ipsc_form":               "An IPSC repeater cannot carry this kind of burst.",
		"mmdvm:311000:build_failed":        "The burst could not be built for MMDVM repeater 311000.",
		"ipsc:311304200:fec_uncorrectable": "The burst for IPSC repeater 311304200 was too damaged to repair.",
	} {
		got, ok := undeliverable.FromOldForm(old)
		assert.True(t, ok, old)
		assert.Equal(t, want, got, old)
	}
	for _, sentence := range []string{"IPSC repeater 314926 was busy on that slot.", "", "Something: else", "ipsc:abc:slot_busy"} {
		_, ok := undeliverable.FromOldForm(sentence)
		assert.False(t, ok, sentence)
	}
}

func TestParseReadsBackEverySentence(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"ipsc", "mmdvm", "openbridge", ""} {
		for _, repeater := range []uint{0, 314926, 4294967295} {
			for _, reason := range append(undeliverable.Reasons(), "") {
				sentence := undeliverable.Sentence(target, repeater, reason)
				got, ok := undeliverable.Parse(sentence)
				assert.True(t, ok, sentence)
				assert.Equal(t, undeliverable.Entry{Target: target, Repeater: repeater, Reason: reason}, got, sentence)
			}
		}
	}
}

func TestParseReadsTheOldForm(t *testing.T) {
	t.Parallel()
	for old, want := range map[string]undeliverable.Entry{
		"ipsc:314926:slot_busy": {Target: "ipsc", Repeater: 314926, Reason: undeliverable.ReasonSlotBusy},
		"ipsc:311304200":        {Target: "ipsc", Repeater: 311304200},
		"ipsc::no_ipsc_form":    {Target: "ipsc", Reason: undeliverable.ReasonNoIPSCForm},
		"newproto:42:new_code":  {Repeater: 42},
	} {
		got, ok := undeliverable.Parse(old)
		assert.True(t, ok, old)
		assert.Equal(t, want, got, old)
	}
}

func TestParseRefusesOtherText(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "Something: else", "ipsc:abc:slot_busy", "IPSC repeater 99999999999 was busy on that slot.", "IPSC repeater 314926 was busy on that slot. And more."} {
		_, ok := undeliverable.Parse(text)
		assert.False(t, ok, text)
	}
}
