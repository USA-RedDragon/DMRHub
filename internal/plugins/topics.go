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

package plugins

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/protobuf/proto"
)

const (
	// TextTopic is the pubsub topic of the text messages that the hub read,
	// as a pluginv1.Text. The stream of each plugin takes those to its IDs.
	TextTopic = "plugins:text"
	// SwitchTopic is the pubsub topic of the changes to the users'
	// switches of plugins: the plugin ID in 4 bytes, then a
	// pluginv1.UserSwitch.
	SwitchTopic = "plugins:switch"
	// APRSTopic is the pubsub topic of the APRS messages from APRS-IS, as a
	// pluginv1.APRSMessage. The stream of each plugin takes those to the
	// addresses it subscribed to.
	APRSTopic = "plugins:aprs"
	// maxSeenTexts bounds the text numbers that a stream keeps to send each
	// text once.
	maxSeenTexts = 1024
)

// formatName returns the name of the text format f in a Text, and false
// for a format that is not a text.
func formatName(f models.MessageFormat) (string, bool) {
	switch f {
	case models.MessageFormatTMS:
		return "TMS", true
	case models.MessageFormatIP:
		return "ETSI IP", true
	case models.MessageFormatAnyTone:
		return "AnyTone", true
	case models.MessageFormatDefinedData:
		return "Defined data", true
	case models.MessageFormatHytera:
		return "Hytera", true
	case models.MessageFormatUDT:
		return "UDT", true
	case models.MessageFormatData, models.MessageFormatUnknown:
	}
	return "", false
}

// PublishText tells the plugins on every replica of msg, when it is a text
// that the hub read whole and right. Give it, with the other notifies, to
// messages.Store.SetNotify.
func PublishText(ps pubsub.PubSub, msg *models.Message) {
	if msg == nil || msg.Text == nil || msg.BadCRC || !msg.ChecksumOK || msg.ID == 0 {
		return
	}
	name, ok := formatName(msg.Format)
	if !ok {
		return
	}
	t := &pluginv1.Text{
		Id: uint64(msg.ID), Source: uint32(msg.SourceID), Destination: uint32(msg.DestinationID), Group: msg.IsGroup, //nolint:gosec // DMR IDs are 24 bits
		Slot: uint32(msg.Slot), Repeater: uint32(msg.RepeaterID), Format: name, Body: *msg.Text, At: msg.ReceivedAt.UnixMilli(), //nolint:gosec // repeater IDs are at most 9 digits
	}
	b, err := proto.Marshal(t)
	if err != nil {
		slog.Error("Plugins: cannot encode a text", "error", err)
		return
	}
	if err := ps.Publish(TextTopic, b); err != nil {
		slog.Error("Plugins: cannot publish a text", "error", err)
	}
}

// PublishSwitch tells the stream of plugin, on any replica, that a user
// set one of its switches.
func PublishSwitch(ps pubsub.PubSub, plugin uint, u *models.UserPluginSwitch) error {
	b, err := proto.Marshal(userSwitchOf(u))
	if err != nil {
		return fmt.Errorf("could not encode a switch: %w", err)
	}
	if err := ps.Publish(SwitchTopic, append(binary.BigEndian.AppendUint32(nil, uint32(plugin)), b...)); err != nil { //nolint:gosec // plugin IDs are small
		return fmt.Errorf("could not publish a switch: %w", err)
	}
	return nil
}

func decodeSwitch(b []byte) (uint, *pluginv1.UserSwitch, bool) {
	if len(b) < 4 {
		return 0, nil, false
	}
	var u pluginv1.UserSwitch
	if proto.Unmarshal(b[4:], &u) != nil {
		return 0, nil, false
	}
	return uint(binary.BigEndian.Uint32(b)), &u, true
}

// PublishAPRS hands m, an APRS message from APRS-IS, to the plugins on
// every replica that subscribed to its addressee.
func PublishAPRS(ps pubsub.PubSub, m *pluginv1.APRSMessage) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return fmt.Errorf("could not encode an APRS message: %w", err)
	}
	if err := ps.Publish(APRSTopic, b); err != nil {
		return fmt.Errorf("could not publish an APRS message: %w", err)
	}
	return nil
}

// switchSpecs returns the switches of h that the hub keeps, and logs the
// others.
func switchSpecs(name string, h *pluginv1.Hello) []models.PluginSwitchSpec {
	out := make([]models.PluginSwitchSpec, 0, len(h.GetSwitches()))
	seen := map[string]bool{}
	for _, sw := range h.GetSwitches() {
		spec := models.PluginSwitchSpec{Key: sw.GetKey(), Title: sw.GetTitle(), Description: sw.GetDescription(), Notice: sw.GetNotice()}
		for _, d := range sw.GetDetails() {
			spec.Details = append(spec.Details, models.PluginSwitchDetail{Label: d.GetLabel(), Value: d.GetValue()})
		}
		if c := sw.GetChoice(); c != nil {
			spec.Choice = &models.PluginSwitchChoice{Label: c.GetLabel(), Options: c.GetOptions(), Preset: c.GetPreset()}
		}
		if err := spec.Check(); err != nil || seen[spec.Key] || len(out) == models.MaxPluginSwitches {
			slog.Warn("Plugins: dropping a switch of a plugin", "plugin", name, "switch", sw.GetKey(), "error", err)
			continue
		}
		seen[spec.Key] = true
		out = append(out, spec)
	}
	return out
}

func sameSpecs(a, b []models.PluginSwitchSpec) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].Title != b[i].Title || a[i].Description != b[i].Description || a[i].Notice != b[i].Notice ||
			!slices.Equal(a[i].Details, b[i].Details) || !sameChoice(a[i].Choice, b[i].Choice) {
			return false
		}
	}
	return true
}

func sameChoice(a, b *models.PluginSwitchChoice) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Label == b.Label && a.Preset == b.Preset && slices.Equal(a.Options, b.Options)
}

// setSubscribe sets what the plugin hears besides the calls to its IDs to
// sub. It follows the topics before it sends the Tracked and the
// UserSwitches, so that the plugin hears what comes after those.
func (w *watcher) setSubscribe(sub *pluginv1.Subscribe) {
	w.heardAll = sub.GetHeard()
	switch want := sub.GetTracking() || w.heardAll; {
	case want && w.heard == nil:
		w.heard = w.s.pubsub.Feed()
		w.heard.Add(HeardTopic)
		w.heard.Add(HeardAtTopic)
	case !want && w.heard != nil:
		_ = w.heard.Close()
		w.heard, w.twins = nil, nil
	}
	w.texts = sub.GetTexts()
	if !w.texts {
		w.seen = nil
	}
	w.follow(TextTopic, w.texts)
	w.setAPRS(sub.GetAprs())
	w.wantSwitches = sub.GetSwitches()
	w.follow(SwitchTopic, w.wantSwitches || len(w.aprsTo) > 0)
	w.setTracking(sub.GetTracking())
	if w.wantSwitches {
		w.sendSwitches()
	}
}

// channels returns the channels of what the plugin subscribed to, nil for
// what it did not.
func (w *watcher) channels() (tracking <-chan []byte, heard, feed <-chan pubsub.Message) {
	if w.tracking != nil {
		tracking = w.tracking.Channel()
	}
	if w.heard != nil {
		heard = w.heard.Channel()
	}
	if w.feed != nil {
		feed = w.feed.Channel()
	}
	return tracking, heard, feed
}

// follow adds topic to the feed of the watcher when on, and removes it
// when not. A message of a removed topic can still come.
func (w *watcher) follow(topic string, on bool) {
	switch {
	case on && !w.topics[topic]:
		if w.feed == nil {
			w.feed, w.topics = w.s.pubsub.Feed(), map[string]bool{}
		}
		w.feed.Add(topic)
		w.topics[topic] = true
	case !on && w.topics[topic]:
		w.feed.Remove(topic)
		delete(w.topics, topic)
	}
}

func (w *watcher) closeFeed() {
	if w.feed != nil {
		_ = w.feed.Close()
		w.feed, w.topics = nil, nil
	}
}

// fromFeed hands m to what the plugin subscribed to.
func (w *watcher) fromFeed(m pubsub.Message, ranges []models.PluginRange) {
	switch m.Topic {
	case TextTopic:
		if w.texts {
			w.text(m.Payload, ranges)
		}
	case SwitchTopic:
		w.switchChanged(m.Payload)
	case APRSTopic:
		w.aprsMessage(m.Payload)
	}
}

// setAPRS sets the APRS addresses of the plugin to those of addresses
// whose callsign is that of a user who turned a switch of it on.
func (w *watcher) setAPRS(addresses []string) {
	w.aprsTo = map[string]bool{}
	if len(addresses) > 0 {
		on, err := optedIn(w.s.db, w.p.ID)
		if err != nil {
			slog.Error("Plugins: cannot list the users of a plugin", "plugin", w.p.Name, "error", err)
		}
		for _, a := range addresses {
			a = strings.ToUpper(a)
			if on[baseCall(a)] {
				w.aprsTo[a] = true
			} else {
				slog.Warn("Plugins: dropping an APRS address of no user of the plugin", "plugin", w.p.Name, "address", a)
			}
		}
	}
	w.follow(APRSTopic, len(w.aprsTo) > 0)
}

func (w *watcher) sendSwitches() {
	on, err := models.PluginSwitchesOn(w.s.db, w.p.ID)
	if err != nil {
		slog.Error("Plugins: cannot list the users of a plugin", "plugin", w.p.Name, "error", err)
		return
	}
	out := &pluginv1.UserSwitches{Switches: make([]*pluginv1.UserSwitch, 0, len(on))}
	for i := range on {
		out.Switches = append(out.Switches, userSwitchOf(&on[i]))
	}
	w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_UserSwitches{UserSwitches: out}})
}

// text sends the text in b when it is to one of the plugin's IDs and did
// not go to the plugin yet.
func (w *watcher) text(b []byte, ranges []models.PluginRange) {
	var t pluginv1.Text
	if proto.Unmarshal(b, &t) != nil || !inRanges(ranges, uint(t.GetDestination())) {
		return
	}
	if w.seen == nil || len(w.seen) >= maxSeenTexts {
		w.seen = map[uint64]struct{}{}
	}
	if _, sent := w.seen[t.GetId()]; sent {
		return
	}
	w.seen[t.GetId()] = struct{}{}
	w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Text{Text: &t}})
}

// switchChanged sends the change in b when it is of the plugin, and drops
// the APRS addresses of a user who has no switch of it on any more.
func (w *watcher) switchChanged(b []byte) {
	plugin, u, ok := decodeSwitch(b)
	if !ok || plugin != w.p.ID {
		return
	}
	if w.wantSwitches {
		w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_UserSwitch{UserSwitch: u}})
	}
	if u.GetOn() || len(w.aprsTo) == 0 {
		return
	}
	on, err := optedIn(w.s.db, w.p.ID)
	if err != nil {
		slog.Error("Plugins: cannot list the users of a plugin", "plugin", w.p.Name, "error", err)
		return
	}
	for a := range w.aprsTo {
		if !on[baseCall(a)] {
			delete(w.aprsTo, a)
		}
	}
	if len(w.aprsTo) == 0 {
		w.follow(APRSTopic, false)
		w.follow(SwitchTopic, w.wantSwitches)
	}
}

// aprsMessage sends the APRS message in b when it is to one of the
// plugin's addresses whose user still has a switch of the plugin on.
func (w *watcher) aprsMessage(b []byte) {
	var m pluginv1.APRSMessage
	if proto.Unmarshal(b, &m) != nil {
		return
	}
	address := strings.ToUpper(strings.TrimSpace(m.GetAddressee()))
	if !w.aprsTo[address] {
		return
	}
	on, err := optedIn(w.s.db, w.p.ID)
	if err != nil || !on[baseCall(address)] {
		delete(w.aprsTo, address)
		return
	}
	w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Aprs{Aprs: &m}})
}
