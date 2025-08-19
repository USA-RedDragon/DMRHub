// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package hbrp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v3"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var subscriptionManager *SubscriptionManager //nolint:golint,gochecknoglobals

type SubscriptionManager struct {
	// stores map[uint]context.CancelFunc indexed by strconv.Itoa(int(radioID))
	subscriptions *xsync.MapOf[uint, *xsync.MapOf[uint, *context.CancelFunc]]
	db            *gorm.DB
}

func GetSubscriptionManager(db *gorm.DB) *SubscriptionManager {
	if subscriptionManager == nil {
		subscriptionManager = &SubscriptionManager{
			subscriptions: xsync.NewMapOf[uint, *xsync.MapOf[uint, *context.CancelFunc]](),
			db:            db,
		}
	}
	return subscriptionManager
}

func (m *SubscriptionManager) CancelSubscription(repeaterID uint, talkgroupID uint, slot dmrconst.Timeslot) {
	radioSubscriptions, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		logging.Errorf("Failed to load radio subscriptions for repeater %d", repeaterID)
		return
	}

	p, err := models.FindRepeaterByID(m.db, repeaterID)
	if err != nil {
		logging.Errorf("Failed to find repeater %d: %s", repeaterID, err)
		return
	}

	// Check the other slot
	dynamicSlot := p.TS2DynamicTalkgroupID
	if slot == dmrconst.TimeslotTwo {
		dynamicSlot = p.TS1DynamicTalkgroupID
	}

	// If the other slot is linked to this talkgroup, don't cancel the subscription
	if dynamicSlot != nil && *dynamicSlot == talkgroupID {
		logging.Errorf("Not cancelling subscription for repeater %d, talkgroup %d, slot %d because the other slot is linked to this talkgroup", p.ID, talkgroupID, slot)
		return
	}

	for _, tg := range p.TS1StaticTalkgroups {
		if tg.ID == talkgroupID {
			return
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		if tg.ID == talkgroupID {
			return
		}
	}
	cancelPtr, ok := radioSubscriptions.LoadAndDelete(talkgroupID)
	if !ok {
		return
	}
	cancel := *cancelPtr
	cancel()
}

func (m *SubscriptionManager) CancelAllSubscriptions() {
	slog.Debug("Cancelling all subscriptions")
	m.subscriptions.Range(func(radioID uint, value *xsync.MapOf[uint, *context.CancelFunc]) bool {
		m.CancelAllRepeaterSubscriptions(radioID)
		return true
	})
}

func (m *SubscriptionManager) CancelAllRepeaterSubscriptions(repeaterID uint) {
	slog.Debug("Cancelling all repeater subscriptions", "repeaterID", repeaterID)
	radioSubs, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		return
	}
	radioSubs.Range(func(tgID uint, value *context.CancelFunc) bool {
		m.CancelSubscription(repeaterID, tgID, dmrconst.TimeslotOne)
		m.CancelSubscription(repeaterID, tgID, dmrconst.TimeslotTwo)
		return true
	})
}

func (m *SubscriptionManager) ListenForCallsOn(pubsub pubsub.PubSub, repeaterID uint, talkgroupID uint) {
	_, span := otel.Tracer("DMRHub").Start(context.Background(), "SubscriptionManager.ListenForCallsOn")
	defer span.End()
	radioSubs, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		return
	}
	_, ok = radioSubs.Load(talkgroupID)
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		radioSubs.Store(talkgroupID, &cancel)
		go m.subscribeTG(newCtx, pubsub, repeaterID, talkgroupID) //nolint:golint,contextcheck
	}
}

func (m *SubscriptionManager) ListenForCalls(pubsub pubsub.PubSub, repeaterID uint) {
	// Subscribe to "packets:repeater:<id>" channel for a dmr.RawDMRPacket
	// This channel is used to get private calls headed to this repeater
	// When a packet is received, we need to publish it to "outgoing" channel
	// with the destination repeater ID as this one
	_, span := otel.Tracer("DMRHub").Start(context.Background(), "SubscriptionManager.ListenForCalls")
	defer span.End()

	_, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		m.subscriptions.Store(repeaterID, xsync.NewMapOf[uint, *context.CancelFunc]())
	}

	radioSubs, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		logging.Error("Failed to load radio subscriptions")
		return
	}

	p, err := models.FindRepeaterByID(m.db, repeaterID)
	if err != nil {
		logging.Errorf("Failed to find repeater %d: %s", repeaterID, err)
		return
	}

	_, ok = radioSubs.Load(repeaterID)
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		radioSubs.Store(repeaterID, &cancel)
		go m.subscribeRepeater(newCtx, pubsub, repeaterID) //nolint:golint,contextcheck
	}

	// Subscribe to pubsub "packets:talkgroup:<id>" channel for each talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(tg.ID, &cancel)
			go m.subscribeTG(newCtx, pubsub, repeaterID, tg.ID) //nolint:golint,contextcheck
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(tg.ID, &cancel)
			go m.subscribeTG(newCtx, pubsub, repeaterID, tg.ID) //nolint:golint,contextcheck
		}
	}
	if p.TS1DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS1DynamicTalkgroupID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(*p.TS1DynamicTalkgroupID, &cancel)
			go m.subscribeTG(newCtx, pubsub, repeaterID, *p.TS1DynamicTalkgroupID) //nolint:golint,contextcheck
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS2DynamicTalkgroupID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(*p.TS2DynamicTalkgroupID, &cancel)
			go m.subscribeTG(newCtx, pubsub, repeaterID, *p.TS2DynamicTalkgroupID) //nolint:golint,contextcheck
		}
	}
}

func (m *SubscriptionManager) ListenForWebsocket(ctx context.Context, pubsub pubsub.PubSub, userID uint) {
	logging.Logf("Listening for websocket for user %d", userID)
	subscription := pubsub.Subscribe("calls")
	defer func() {
		err := subscription.Unsubscribe()
		if err != nil {
			logging.Errorf("Error unsubscribing from calls: %s", err)
		}
		err = subscription.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub connection: %s", err)
		}
	}()
	pubsubChannel := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			logging.Logf("Websocket context done for user %d", userID)
			return
		case msg := <-pubsubChannel:
			var call models.Call
			err := json.Unmarshal(msg, &call)
			if err != nil {
				logging.Errorf("Error unmarshalling call: %s", err)
				continue
			}

			userExists, err := models.UserIDExists(m.db, userID)
			if err != nil {
				logging.Errorf("Error checking if user exists: %s", err)
				continue
			}

			if !userExists {
				logging.Errorf("User %d does not exist", userID)
				continue
			}

			user, err := models.FindUserByID(m.db, userID)
			if err != nil {
				logging.Errorf("Error finding user: %s", err)
				continue
			}

			for _, p := range user.Repeaters {
				want, _ := p.WantRXCall(call)
				if want || call.User.ID == userID || call.DestinationID == p.OwnerID {
					// copy call into a jsonCallResponse
					var jsonCall apimodels.WSCallResponse
					jsonCall.ID = call.ID
					jsonCall.User.ID = call.User.ID
					jsonCall.User.Callsign = call.User.Callsign
					jsonCall.StartTime = call.StartTime
					jsonCall.Duration = call.Duration
					jsonCall.Active = call.Active
					jsonCall.TimeSlot = call.TimeSlot
					jsonCall.GroupCall = call.GroupCall
					if call.IsToTalkgroup {
						jsonCall.ToTalkgroup.ID = call.ToTalkgroup.ID
						jsonCall.ToTalkgroup.Name = call.ToTalkgroup.Name
						jsonCall.ToTalkgroup.Description = call.ToTalkgroup.Description
					}
					if call.IsToUser {
						jsonCall.ToUser.ID = call.ToUser.ID
						jsonCall.ToUser.Callsign = call.ToUser.Callsign
					}
					if call.IsToRepeater {
						jsonCall.ToRepeater.RadioID = call.ToRepeater.ID
						jsonCall.ToRepeater.Callsign = call.ToRepeater.Callsign
					}
					jsonCall.IsToTalkgroup = call.IsToTalkgroup
					jsonCall.IsToUser = call.IsToUser
					jsonCall.IsToRepeater = call.IsToRepeater
					jsonCall.Loss = call.Loss
					jsonCall.Jitter = call.Jitter
					jsonCall.BER = call.BER
					jsonCall.RSSI = call.RSSI
					// Publish the call JSON to pubsub
					callJSON, err := json.Marshal(jsonCall)
					if err != nil {
						logging.Errorf("Error marshalling call JSON: %v", err)
						break
					}
					pubsub.Publish(fmt.Sprintf("calls:%d", userID), callJSON)
					break
				}
			}
		}
	}
}

func (m *SubscriptionManager) subscribeRepeater(ctx context.Context, pubsub pubsub.PubSub, repeaterID uint) {
	slog.Debug("Listening for calls on repeater", "repeaterID", repeaterID)
	subscription := pubsub.Subscribe(fmt.Sprintf("hbrp:packets:repeater:%d", repeaterID))
	defer func() {
		err := subscription.Unsubscribe()
		if err != nil {
			logging.Errorf("Error unsubscribing from hbrp:packets:repeater:%d: %s", repeaterID, err)
		}
		err = subscription.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub connection: %s", err)
		}
	}()
	pubsubChannel := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			slog.Debug("Context canceled, stopping subscription to hbrp:packets:repeater", "repeaterID", repeaterID)
			radioSubs, ok := m.subscriptions.Load(repeaterID)
			if ok {
				radioSubs.Delete(repeaterID)
			}
			return
		case msg := <-pubsubChannel:
			rawPacket := models.RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg([]byte(msg))
			if err != nil {
				logging.Errorf("Failed to unmarshal raw packet: %s", err)
				continue
			}
			// This packet is already for us and we don't want to modify the slot
			packet, ok := models.UnpackPacket(rawPacket.Data)
			if !ok {
				logging.Errorf("Failed to unpack packet")
				continue
			}
			packet.Repeater = repeaterID
			pubsub.Publish("hbrp:outgoing:noaddr", packet.Encode())
		}
	}
}

func (m *SubscriptionManager) subscribeTG(ctx context.Context, pubsub pubsub.PubSub, repeaterID uint, tg uint) {
	if tg == 0 {
		return
	}
	slog.Debug("Listening for calls on talkgroup", "repeaterID", repeaterID, "talkgroupID", tg)
	subscription := pubsub.Subscribe(fmt.Sprintf("hbrp:packets:talkgroup:%d", tg))
	defer func() {
		err := subscription.Unsubscribe()
		if err != nil {
			logging.Errorf("Error unsubscribing from hbrp:packets:talkgroup:%d: %s", tg, err)
		}
		err = subscription.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub connection: %s", err)
		}
	}()
	pubsubChannel := subscription.Channel()

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Context canceled, stopping subscription to hbrp:packets:talkgroup", "repeaterID", repeaterID, "talkgroupID", tg)
			radioSubs, ok := m.subscriptions.Load(repeaterID)
			if ok {
				radioSubs.Delete(tg)
			}
			return
		case msg := <-pubsubChannel:
			rawPacket := models.RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg([]byte(msg))
			if err != nil {
				logging.Errorf("Failed to unmarshal raw packet: %s", err)
				continue
			}
			packet, ok := models.UnpackPacket(rawPacket.Data)
			if !ok {
				logging.Errorf("Failed to unpack packet")
				continue
			}

			if packet.Repeater == repeaterID {
				continue
			}

			p, err := models.FindRepeaterByID(m.db, repeaterID)
			if err != nil {
				logging.Errorf("Failed to find repeater %d: %s", repeaterID, err)
				continue
			}
			want, slot := p.WantRX(packet)
			if want {
				// This packet is for the repeater's dynamic talkgroup
				// We need to send it to the repeater
				packet.Repeater = p.ID
				packet.Slot = slot
				pubsub.Publish("hbrp:outgoing:noaddr", packet.Encode())
			} else {
				// We're subscribed but don't want this packet? With a talkgroup that can only mean we're unlinked, so we should unsubscribe
				err := subscription.Unsubscribe()
				if err != nil {
					logging.Errorf("Error unsubscribing from hbrp:packets:talkgroup:%d: %s", tg, err)
				}
				err = subscription.Close()
				if err != nil {
					logging.Errorf("Error closing pubsub connection: %s", err)
				}
				return
			}
		}
	}
}
