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

package dmr_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	respSender   = uint(1000001)
	respAnswerer = uint(1000002)
	respAdmin    = uint(1000003)
	respRpt1     = uint(100001)
	respRpt2     = uint(100002)
	respWait     = 15 * time.Second
)

type responseNet struct {
	stack  *testutils.IntegrationStack
	r1, r2 *testutils.MMDVMClient
	router *gin.Engine
	jar    testutils.CookieJar
	stream uint
}

// startResponseNet connects the repeater of the sender to the first replica
// and the repeater of the answering radio to the second replica when split
// is set, or to the first one too when it is not, and makes the answering
// radio heard on its repeater.
func startResponseNet(t *testing.T, stack *testutils.IntegrationStack, split bool) *responseNet {
	t.Helper()
	stack.SeedUser(t, respSender, "N0CALL")
	stack.SeedUser(t, respAnswerer, "N1CALL")
	admin := testutils.SeedAccount(t, stack.DB, respAdmin, "N2CALL", "admin2")
	require.NoError(t, stack.DB.Model(&admin).Update("admin", true).Error)
	stack.SeedMMDVMRepeater(t, respRpt1, respSender, testPassword)
	stack.SeedMMDVMRepeater(t, respRpt2, respAnswerer, testPassword)

	second := ""
	if split {
		second = stack.SpawnSecondReplica(t).MMDVMAddr
	}
	stack.StartServers(t)
	if !split {
		second = stack.MMDVMAddr
	}

	client := func(id uint, at string) *testutils.MMDVMClient {
		c := testutils.NewMMDVMClient(uint32(id), "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(at))
		require.NoError(t, c.WaitReady(handshakeWait))
		return c
	}
	n := &responseNet{stack: stack, r1: client(respRpt1, stack.MMDVMAddr), r2: client(respRpt2, second), stream: 9100}
	time.Sleep(settleDuration)

	ready := &atomic.Bool{}
	ready.Store(true)
	n.router = internalhttp.CreateRouter(context.Background(), stack.Config, stack.Hub, stack.DB, stack.PubSub, ready, nil, "test", "deadbeef")
	n.jar = testutils.Login(t, n.router, "admin2")

	n.send(t, n.r2, respAnswerer, respSender, []layer2.RawBurst{respPreamble(t, respAnswerer, respSender)})
	time.Sleep(settleDuration)
	n.r1.Drain(100 * time.Millisecond)
	n.r2.Drain(100 * time.Millisecond)
	return n
}

func (n *responseNet) send(t *testing.T, c *testutils.MMDVMClient, src, dst uint, bursts []layer2.RawBurst) uint {
	t.Helper()
	n.stream++
	for i, b := range bursts {
		require.NoError(t, c.SendDMRD(models.Packet{
			Signature:   string(dmrconst.CommandDMRD),
			Seq:         uint(i),
			Src:         src,
			Dst:         dst,
			Slot:        true,
			FrameType:   dmrconst.FrameDataSync,
			DTypeOrVSeq: uint(b.SlotType().DataType),
			StreamID:    n.stream,
			DMRData:     b,
			BER:         -1,
			RSSI:        -1,
		}))
		time.Sleep(20 * time.Millisecond)
	}
	return n.stream
}

// expectRouted checks that c got the bursts of stream, in order.
func expectRouted(t *testing.T, c *testutils.MMDVMClient, stream uint, want []layer2.RawBurst, what string) {
	t.Helper()
	got := ofStream(c.Drain(drainWait), stream)
	require.Len(t, got, len(want), "%s is routed", what)
	for i := range want {
		assert.Equal(t, [33]byte(want[i]), got[i].DMRData, "%s burst %d", what, i)
	}
}

func (n *responseNet) lastheard(t *testing.T, user uint) []apimodels.WSCallResponse {
	t.Helper()
	w := testutils.Do(t, n.router, n.jar, http.MethodGet, fmt.Sprintf("/api/v1/lastheard/user/%d?limit=50", user), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Calls []apimodels.WSCallResponse `json:"calls"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.Calls
}

func (n *responseNet) adminMessages(t *testing.T) []apimodels.AdminMessage {
	t.Helper()
	w := testutils.Do(t, n.router, n.jar, http.MethodGet, "/api/v1/admin/messages", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out apimodels.AdminMessageList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.Messages
}

func callOfStream(t *testing.T, stack *testutils.IntegrationStack, stream uint) *models.Call {
	t.Helper()
	var calls []models.Call
	require.NoError(t, stack.DB.Where("stream_id = ? AND active = ?", stream, false).Find(&calls).Error)
	if len(calls) != 1 {
		return nil
	}
	return &calls[0]
}

func respSMS(t *testing.T) []layer2.RawBurst {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatTMS, Source: uint32(respSender), Destination: uint32(respAnswerer), Confirmed: true, Body: []byte("copy, 5x9")}, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return bursts
}

// respACK returns the bursts with which a radio answers bursts.
func respACK(t *testing.T, bursts []layer2.RawBurst) []layer2.RawBurst {
	t.Helper()
	radio, err := data.NewReceiver(&data.ReceiverConfig{Respond: true, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	var acks []layer2.RawBurst
	var events []data.Event
	for i := range bursts {
		events = radio.Burst(time.Duration(i)*60*time.Millisecond, &bursts[i], layer2.Hint{}, events[:0])
		for _, ev := range events {
			if ev.Kind == data.EventRespond {
				acks = append(acks, ev.Burst)
			}
		}
	}
	require.NotEmpty(t, acks)
	return acks
}

func respCSBK(t *testing.T, c *pdu.CSBK) layer2.RawBurst {
	t.Helper()
	c.LastBlock = true
	raw, err := layer2.BuildBurst(c, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return raw
}

func respPreamble(t *testing.T, src, dst uint) layer2.RawBurst {
	t.Helper()
	var c pdu.CSBK
	require.NoError(t, c.SetPreamble(pdu.PreamblePDU{Data: true, TargetAddress: uint32(dst), SourceAddress: uint32(src)}))
	return respCSBK(t, &c)
}

func respRadioCheck(t *testing.T, answer bool) layer2.RawBurst {
	t.Helper()
	var c pdu.CSBK
	require.NoError(t, c.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{Answer: answer, RequesterAddress: uint32(respSender), CheckedAddress: uint32(respAnswerer)}))
	return respCSBK(t, &c)
}

// TestResponseMatchesAcrossReplicas sends a confirmed SMS and a radio check
// from a repeater to a radio on another repeater, which answers them, with
// both repeaters on one replica and with each on its own replica. The
// answers must reach the sender, and the ACK must be set on the call row
// and the stored message of the SMS and on the row of the radio check, the
// same in both cases.
func TestResponseMatchesAcrossReplicas(t *testing.T) {
	t.Parallel()
	for _, split := range []bool{false, true} {
		name := "one replica"
		if split {
			name = "two replicas"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				n := startResponseNet(t, stack, split)

				msg := respSMS(t)
				msgStream := n.send(t, n.r1, respSender, respAnswerer, msg)
				expectRouted(t, n.r2, msgStream, msg, "the message")
				ack := respACK(t, msg)
				ackStream := n.send(t, n.r2, respAnswerer, respSender, ack)
				expectRouted(t, n.r1, ackStream, ack, "the ACK")

				check := []layer2.RawBurst{respRadioCheck(t, false)}
				checkStream := n.send(t, n.r1, respSender, respAnswerer, check)
				expectRouted(t, n.r2, checkStream, check, "the radio check")
				answer := []layer2.RawBurst{respRadioCheck(t, true)}
				answerStream := n.send(t, n.r2, respAnswerer, respSender, answer)
				expectRouted(t, n.r1, answerStream, answer, "the radio check answer")

				var msgCall, ackCall, checkCall *models.Call
				require.Eventually(t, func() bool {
					msgCall, ackCall, checkCall = callOfStream(t, stack, msgStream), callOfStream(t, stack, ackStream), callOfStream(t, stack, checkStream)
					return msgCall != nil && ackCall != nil && checkCall != nil
				}, respWait, 100*time.Millisecond, "the rows end")
				stack.MessageStore.Flush()

				assert.Equal(t, "sms", msgCall.SubKind)
				assert.True(t, msgCall.Confirmed)
				assert.Equal(t, models.CallResponseACK, msgCall.Response, "the row of the message has the ACK")
				assert.Equal(t, "response", ackCall.SubKind)
				assert.Equal(t, models.CallResponseACK, ackCall.Response)
				assert.Equal(t, "radio_check", checkCall.SubKind)
				assert.Equal(t, models.CallResponseACK, checkCall.Response, "the row of the radio check has the answer")
				var answers int64
				require.NoError(t, stack.DB.Model(&models.Call{}).Where("stream_id = ?", answerStream).Count(&answers).Error)
				assert.Zero(t, answers, "the answer makes no row of its own")

				require.Eventually(t, func() bool {
					var m models.Message
					return stack.DB.Where("source_id = ? AND destination_id = ?", respSender, respAnswerer).First(&m).Error == nil && m.Response == models.CallResponseACK
				}, respWait, 100*time.Millisecond, "the stored message has the ACK")
				var stored []models.Message
				require.NoError(t, stack.DB.Find(&stored).Error)
				require.Len(t, stored, 1, "only the message is stored")
				require.NotNil(t, stored[0].TransmissionID)
				assert.Equal(t, msgCall.ID, *stored[0].TransmissionID)

				msgs := n.adminMessages(t)
				if assert.Len(t, msgs, 1) {
					assert.Equal(t, string(models.CallResponseACK), msgs[0].Response, "the API shows the ACK on the message")
					assert.Equal(t, "copy, 5x9", *msgs[0].Text)
				}
				byID := map[uint]apimodels.WSCallResponse{}
				for _, c := range n.lastheard(t, respSender) {
					byID[c.ID] = c
				}
				if assert.Contains(t, byID, msgCall.ID) {
					assert.Equal(t, models.CallResponseACK, byID[msgCall.ID].Response, "the API shows the ACK on the row of the message")
				}
				if assert.Contains(t, byID, checkCall.ID) {
					assert.Equal(t, models.CallResponseACK, byID[checkCall.ID].Response, "the API shows the answer on the row of the radio check")
				}
			})
		})
	}
}
