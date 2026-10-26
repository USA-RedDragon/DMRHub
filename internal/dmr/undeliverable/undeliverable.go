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

// Package undeliverable words why a target server could not carry a burst.
// Metrics and logs keep the reason codes; Last Heard stores the sentences.
package undeliverable

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The reasons that a target server reports for a burst that it cannot
// carry. They are stable: the undeliverable metric labels its counts with
// them.
const (
	ReasonInvalidDataType = "invalid_data_type"
	ReasonNoIPSCForm      = "no_ipsc_form"
	ReasonFrameType3      = "frame_type_3"
	ReasonFEC             = "fec_uncorrectable"
	ReasonSlotType        = "slot_type"
	ReasonStreamID        = "stream_id_range"
	ReasonBuild           = "build_failed"
	ReasonEncode          = "encode_failed"
	ReasonDataInVoice     = "data_in_voice"
	ReasonUndecodable     = "undecodable"
	ReasonSlotBusy        = "slot_busy"
)

func sentenceFormat(reason string) string {
	switch reason {
	case ReasonInvalidDataType:
		return "the burst for %s had an invalid data type."
	case ReasonNoIPSCForm:
		return "%s cannot carry this kind of burst."
	case ReasonFrameType3:
		return "the burst for %s had an unknown frame type."
	case ReasonFEC:
		return "the burst for %s was too damaged to repair."
	case ReasonSlotType:
		return "the burst could not be rebuilt for %s."
	case ReasonStreamID:
		return "the call ID does not fit the link to %s."
	case ReasonBuild:
		return "the burst could not be built for %s."
	case ReasonEncode:
		return "the burst could not be encoded for %s."
	case ReasonDataInVoice:
		return "a data burst for %s came in the middle of a voice call."
	case ReasonUndecodable:
		return "the burst for %s could not be read."
	case ReasonSlotBusy:
		return "%s was busy on that slot."
	default:
		return "the burst could not be prepared for %s."
	}
}

// Reasons returns every reason that Sentence words.
func Reasons() []string {
	return []string{
		ReasonInvalidDataType, ReasonNoIPSCForm, ReasonFrameType3, ReasonFEC,
		ReasonSlotType, ReasonStreamID, ReasonBuild, ReasonEncode,
		ReasonDataInVoice, ReasonUndecodable, ReasonSlotBusy,
	}
}

func targetName(target string) (string, bool) {
	switch target {
	case "ipsc":
		return "IPSC repeater", true
	case "mmdvm":
		return "MMDVM repeater", true
	case "openbridge":
		return "OpenBridge peer", true
	default:
		return "repeater", false
	}
}

// Sentence words why the target server, such as "ipsc", could not carry a
// burst to the repeater for reason, such as "IPSC repeater 314926 was busy
// on that slot.". A repeater of 0 is one that the report does not name.
func Sentence(target string, repeater uint, reason string) string {
	kind, known := targetName(target)
	who := "an " + kind
	switch {
	case repeater != 0:
		who = kind + " " + strconv.FormatUint(uint64(repeater), 10)
	case !known:
		who = "a " + kind
	}
	return capitalize(fmt.Sprintf(sentenceFormat(reason), who))
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

var oldForm = regexp.MustCompile(`^([a-z0-9]+):([0-9]*)(?::([a-z0-9_]+))?$`)

// FromOldForm turns an entry of the old form "<target>:<repeater>:<reason>",
// such as "ipsc:314926:slot_busy", into its sentence. It reports false
// for an entry of any other form.
func FromOldForm(entry string) (string, bool) {
	m := oldForm.FindStringSubmatch(entry)
	if m == nil {
		return "", false
	}
	var repeater uint64
	if m[2] != "" {
		var err error
		repeater, err = strconv.ParseUint(m[2], 10, 32)
		if err != nil {
			return "", false
		}
	}
	return Sentence(m[1], uint(repeater), m[3]), true
}

// Entry is an undeliverable sentence read back into its parts.
type Entry struct {
	// Target is the target server, such as "ipsc", or "" for one that
	// the sentence does not name.
	Target string
	// Repeater is the repeater that the sentence names, or 0.
	Repeater uint
	// Reason is the reason code, such as ReasonSlotBusy, or "" for a
	// reason that the sentence does not name.
	Reason string
}

func targetOf(kind string) string {
	switch strings.ToLower(kind) {
	case "ipsc repeater":
		return "ipsc"
	case "mmdvm repeater":
		return "mmdvm"
	case "openbridge peer":
		return "openbridge"
	default:
		return ""
	}
}

func sentenceReasons() []string {
	return append(Reasons(), "")
}

func sentencePattern() string {
	const who = `(?:an? )?(IPSC repeater|MMDVM repeater|OpenBridge peer|repeater)(?: ([0-9]+))?`
	reasons := sentenceReasons()
	alts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		alts = append(alts, strings.Replace(regexp.QuoteMeta(sentenceFormat(reason)), "%s", who, 1))
	}
	return `(?i)^(?:` + strings.Join(alts, "|") + `)$`
}

var sentenceForm = regexp.MustCompile(sentencePattern())

// Parse reads a sentence that Sentence wrote, or an entry of the old form
// that FromOldForm reads, back into its parts. It reports false for any
// other text.
func Parse(text string) (Entry, bool) {
	if m := sentenceForm.FindStringSubmatch(text); m != nil {
		for i, reason := range sentenceReasons() {
			kind, id := m[1+2*i], m[2+2*i]
			if kind == "" {
				continue
			}
			e := Entry{Target: targetOf(kind), Reason: reason}
			if id != "" {
				n, err := strconv.ParseUint(id, 10, 32)
				if err != nil {
					return Entry{}, false
				}
				e.Repeater = uint(n)
			}
			return e, true
		}
	}
	m := oldForm.FindStringSubmatch(text)
	if m == nil {
		return Entry{}, false
	}
	var e Entry
	if slices.Contains(Reasons(), m[3]) {
		e.Reason = m[3]
	}
	if _, known := targetName(m[1]); known {
		e.Target = m[1]
	}
	if m[2] != "" {
		id, err := strconv.ParseUint(m[2], 10, 32)
		if err != nil {
			return Entry{}, false
		}
		e.Repeater = uint(id)
	}
	return e, true
}
