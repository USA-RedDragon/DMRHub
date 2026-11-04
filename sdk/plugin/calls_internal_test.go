// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/protobuf/proto"
)

func welcomeWith(s pluginv1.PluginService_ConnectServer, features ...pluginv1.Feature) {
	_ = s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Welcome{Welcome: &pluginv1.Welcome{Name: "P", Features: features}}})
}

func answer(s pluginv1.PluginService_ConnectServer, a *pluginv1.Answer) {
	_ = s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Answer{Answer: a}})
}

func dial(t *testing.T, opts Options) *Conn {
	t.Helper()
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestAConnGetsAnswersWithoutRecv checks that the answer to a request
// reaches it while no goroutine calls Recv, so that a plugin can make a
// request while it handles a message from Recv, and that answers come to
// their own requests in any order.
func TestAConnGetsAnswersWithoutRecv(t *testing.T) {
	t.Parallel()
	firstIn := make(chan struct{})
	opts := startHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		welcomeWith(s, pluginv1.Feature_FEATURE_STORE, pluginv1.Feature_FEATURE_LOOKUP)
		first, _ := s.Recv()
		close(firstIn)
		second, _ := s.Recv()
		answer(s, &pluginv1.Answer{Id: second.GetRequest().GetId(), Result: &pluginv1.Answer_User{User: &pluginv1.User{Callsign: "N2CALL"}}})
		answer(s, &pluginv1.Answer{Id: first.GetRequest().GetId(), Result: &pluginv1.Answer_StoreValue{StoreValue: &pluginv1.StoreValue{Value: []byte("v")}}})
		third, _ := s.Recv()
		answer(s, &pluginv1.Answer{Id: third.GetRequest().GetId(), Code: pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, Error: "no such key"})
		<-s.Context().Done()
		return nil
	})
	c := dial(t, opts)
	type res struct {
		v   []byte
		err error
	}
	got := make(chan res, 1)
	go func() {
		v, err := c.StoreGet(t.Context(), "k")
		got <- res{v, err}
	}()
	within(t, firstIn, "the request of StoreGet")
	u, err := c.LookupCallsign(t.Context(), "n2call")
	if err != nil || u.GetCallsign() != "N2CALL" {
		t.Fatalf("LookupCallsign = %v, %v", u, err)
	}
	r := within(t, got, "the answer of StoreGet")
	if r.err != nil || string(r.v) != "v" {
		t.Fatalf("StoreGet = %q, %v", r.v, r.err)
	}
	_, err = c.StoreGet(t.Context(), "gone")
	var ae *AnswerError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &ae) || ae.Reason != "no such key" {
		t.Fatalf("StoreGet of a missing key = %v, want ErrNotFound with the reason", err)
	}
}

func TestAConnAsksOnlyForWhatItsHubServes(t *testing.T) {
	t.Parallel()
	got := make(chan *pluginv1.ConnectRequest, 1)
	opts := startHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		welcomeWith(s, pluginv1.Feature_FEATURE_STORE)
		m, _ := s.Recv()
		got <- m
		<-s.Context().Done()
		return nil
	})
	c := dial(t, opts)
	if !c.Has(pluginv1.Feature_FEATURE_STORE) || c.Has(pluginv1.Feature_FEATURE_SEND_TEXT) {
		t.Fatal("Has does not follow the Welcome")
	}
	if _, err := c.SendText(t.Context(), 4700, 1000001, "hi"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SendText = %v, want ErrUnsupported", err)
	}
	if err := c.Subscribe(&pluginv1.Subscribe{Tracking: true, Texts: true}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Subscribe = %v, want ErrUnsupported", err)
	}
	if err := c.Subscribe(&pluginv1.Subscribe{Tracking: true}); err != nil {
		t.Fatal(err)
	}
	if m := within(t, got, "the subscription"); !proto.Equal(m.GetSubscribe(), &pluginv1.Subscribe{Tracking: true}) {
		t.Fatalf("the hub got %v, want only the subscription it serves", m)
	}
}

func TestARequestWhoseStreamEndsIsLost(t *testing.T) {
	t.Parallel()
	opts := startHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		welcomeWith(s, pluginv1.Feature_FEATURE_STORE)
		_, _ = s.Recv()
		return errRefused
	})
	c := dial(t, opts)
	if err := c.StorePut(t.Context(), "k", nil, 0); !errors.Is(err, ErrLost) {
		t.Fatalf("StorePut = %v, want ErrLost", err)
	}
	if err := c.StoreDelete(t.Context(), "k"); !errors.Is(err, ErrLost) {
		t.Fatalf("StoreDelete after the end = %v, want ErrLost", err)
	}
}

// TestAConnThatMovesSubscribesAgainAndFollowsTheFeaturesOfTheNewReplica
// moves a plugin from a replica of a new version to one of an old version,
// as during a rolling upgrade or a rollback. The new stream gets the whole
// last Subscribe, and Has then follows the old replica.
func TestAConnThatMovesSubscribesAgainAndFollowsTheFeaturesOfTheNewReplica(t *testing.T) {
	t.Parallel()
	moved := make(chan *pluginv1.ConnectRequest, 1)
	subscribed := make(chan struct{})
	opts := startHub(t,
		func(s pluginv1.PluginService_ConnectServer) error {
			welcomeWith(s, pluginv1.Feature_FEATURE_HEARD, pluginv1.Feature_FEATURE_TEXTS)
			_, _ = s.Recv()
			close(subscribed)
			draining(s)
			<-s.Context().Done()
			return nil
		},
		func(s pluginv1.PluginService_ConnectServer) error {
			welcome(s)
			m, _ := s.Recv()
			moved <- m
			sendBurst(s, 1)
			<-s.Context().Done()
			return nil
		},
	)
	c := dial(t, opts)
	want := &pluginv1.Subscribe{Heard: true, Texts: true}
	if err := c.Subscribe(want); err != nil {
		t.Fatal(err)
	}
	within(t, subscribed, "the subscription on the first stream")
	if m, err := recvWithin(t, c); err != nil || m.GetBurst() == nil {
		t.Fatalf("Recv = %v, %v, want the burst of the new stream", m, err)
	}
	if m := within(t, moved, "the subscription on the new stream"); m.GetSubscribe() == nil || !proto.Equal(m.GetSubscribe(), &pluginv1.Subscribe{}) {
		t.Fatalf("the new stream got %v, want the Subscribe without what the old replica does not serve", m)
	}
	if c.Has(pluginv1.Feature_FEATURE_TEXTS) {
		t.Fatal("Has follows the replica the plugin left")
	}
}

// TestAnAnswerComesWhileAMessageWaitsForRecv sends a burst before the
// answer to a request that a plugin makes while it does not call Recv,
// as it does while it handles a message.
func TestAnAnswerComesWhileAMessageWaitsForRecv(t *testing.T) {
	t.Parallel()
	opts := startHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		welcomeWith(s, pluginv1.Feature_FEATURE_STORE)
		r, _ := s.Recv()
		sendBurst(s, 1)
		sendBurst(s, 2)
		answer(s, &pluginv1.Answer{Id: r.GetRequest().GetId(), Result: &pluginv1.Answer_StoreValue{StoreValue: &pluginv1.StoreValue{Value: []byte("v")}}})
		<-s.Context().Done()
		return nil
	})
	c := dial(t, opts)
	got := make(chan error, 1)
	go func() {
		_, err := c.StoreGet(t.Context(), "k")
		got <- err
	}()
	if err := within(t, got, "the answer behind two bursts"); err != nil {
		t.Fatal(err)
	}
	for seq := uint32(1); seq <= 2; seq++ {
		if m, err := recvWithin(t, c); err != nil || m.GetBurst().GetSequence() != seq {
			t.Fatalf("Recv = %v, %v, want burst %d", m, err, seq)
		}
	}
}

func sendText(s pluginv1.PluginService_ConnectServer, id uint64) {
	_ = s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Text{Text: &pluginv1.Text{Id: id, Body: "INBOX"}}})
}

// TestATextOnBothStreamsOfAMoveComesOnce gives the same text on the old
// and the new stream of a move, as the hub does in the grace of a drain.
func TestATextOnBothStreamsOfAMoveComesOnce(t *testing.T) {
	t.Parallel()
	newUp := make(chan struct{})
	opts := startHub(t,
		func(s pluginv1.PluginService_ConnectServer) error {
			welcomeWith(s, pluginv1.Feature_FEATURE_TEXTS)
			draining(s)
			<-newUp
			sendText(s, 5)
			<-s.Context().Done()
			return nil
		},
		func(s pluginv1.PluginService_ConnectServer) error {
			welcomeWith(s, pluginv1.Feature_FEATURE_TEXTS)
			close(newUp)
			sendText(s, 5)
			sendText(s, 6)
			<-s.Context().Done()
			return nil
		},
	)
	c := dial(t, opts)
	var ids []uint64
	for len(ids) == 0 || ids[len(ids)-1] != 6 {
		m, err := recvWithin(t, c)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.GetText().GetId())
	}
	if len(ids) != 2 || ids[0] != 5 {
		t.Fatalf("got the texts %v, want 5 once, then 6", ids)
	}
}

func TestAReplyGoesOnlyToAHubThatAnswersInTheFormatOfTheText(t *testing.T) {
	t.Parallel()
	got := make(chan *pluginv1.ConnectRequest, 2)
	hub := func(features ...pluginv1.Feature) func(pluginv1.PluginService_ConnectServer) error {
		return func(s pluginv1.PluginService_ConnectServer) error {
			welcomeWith(s, features...)
			for {
				m, err := s.Recv()
				if err != nil {
					return fmt.Errorf("the stream ended: %w", err)
				}
				got <- m
				answer(s, &pluginv1.Answer{Id: m.GetRequest().GetId(), Result: &pluginv1.Answer_TextSent{TextSent: &pluginv1.TextSent{Format: "TMS"}}})
			}
		}
	}
	opts := startHub(t, hub(pluginv1.Feature_FEATURE_SEND_TEXT), hub(pluginv1.Feature_FEATURE_SEND_TEXT, pluginv1.Feature_FEATURE_PROMPT))
	text := &pluginv1.Text{Id: 41, Source: 1000001, Destination: 5000, Format: "TMS", Body: "STATUS"}
	old := dial(t, opts)
	if _, err := old.ReplyText(t.Context(), 5000, text, "TS2"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ReplyText to a hub without FEATURE_PROMPT = %v, want ErrUnsupported", err)
	}
	if err := old.PlayPrompt(t.Context(), &pluginv1.PlayPrompt{Source: 5000, Radio: 1000001, Slot: 2, Clips: []string{"digit-2"}}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("PlayPrompt to a hub without FEATURE_PROMPT = %v, want ErrUnsupported", err)
	}
	_ = old.Close()
	c := dial(t, opts)
	format, err := c.ReplyText(t.Context(), 5000, text, "TS2")
	if err != nil || format != "TMS" {
		t.Fatalf("ReplyText = %q, %v", format, err)
	}
	m := within(t, got, "the reply")
	want := &pluginv1.SendText{Source: 5000, Radio: 1000001, Body: "TS2", ReplyTo: proto.Uint64(41)}
	if !proto.Equal(m.GetRequest().GetSendText(), want) {
		t.Fatalf("the hub got %v, want %v", m.GetRequest().GetSendText(), want)
	}
}
