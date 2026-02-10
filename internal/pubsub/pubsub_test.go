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

package pubsub_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
)

func makeTestPubSub(t *testing.T) pubsub.PubSub {
	t.Helper()
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	ps, err := pubsub.MakePubSub(context.Background(), &defConfig)
	if err != nil {
		t.Fatalf("Failed to create pubsub: %v", err)
	}
	t.Cleanup(func() {
		_ = ps.Close()
	})
	return ps
}

func TestPubSubPublishAndSubscribe(t *testing.T) {
	t.Parallel()
	ps := makeTestPubSub(t)

	sub := ps.Subscribe("test-topic")
	defer func() { _ = sub.Close() }()

	msg := []byte("hello world")
	err := ps.Publish("test-topic", msg)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case received := <-sub.Channel():
		if string(received) != string(msg) {
			t.Errorf("Expected '%s', got '%s'", string(msg), string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for message")
	}
}

func TestPubSubMultipleMessages(t *testing.T) {
	t.Parallel()
	ps := makeTestPubSub(t)

	sub := ps.Subscribe("multi")
	defer func() { _ = sub.Close() }()

	messages := []string{"msg1", "msg2", "msg3"}
	for _, m := range messages {
		if err := ps.Publish("multi", []byte(m)); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
	}

	for _, expected := range messages {
		select {
		case received := <-sub.Channel():
			if string(received) != expected {
				t.Errorf("Expected '%s', got '%s'", expected, string(received))
			}
		case <-time.After(time.Second):
			t.Fatalf("Timed out waiting for message '%s'", expected)
		}
	}
}

func TestPubSubDifferentTopics(t *testing.T) {
	t.Parallel()
	ps := makeTestPubSub(t)

	sub1 := ps.Subscribe("topic1")
	defer func() { _ = sub1.Close() }()
	sub2 := ps.Subscribe("topic2")
	defer func() { _ = sub2.Close() }()

	_ = ps.Publish("topic1", []byte("for-topic1"))
	_ = ps.Publish("topic2", []byte("for-topic2"))

	select {
	case received := <-sub1.Channel():
		if string(received) != "for-topic1" {
			t.Errorf("topic1: Expected 'for-topic1', got '%s'", string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out on topic1")
	}

	select {
	case received := <-sub2.Channel():
		if string(received) != "for-topic2" {
			t.Errorf("topic2: Expected 'for-topic2', got '%s'", string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out on topic2")
	}
}

func TestPubSubClose(t *testing.T) {
	t.Parallel()
	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}
	ps, err := pubsub.MakePubSub(context.Background(), &defConfig)
	if err != nil {
		t.Fatalf("Failed to create pubsub: %v", err)
	}
	_ = ps.Subscribe("topic")
	err = ps.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestPubSubSubscribeBeforePublish(t *testing.T) {
	t.Parallel()
	ps := makeTestPubSub(t)

	// Subscribe first, then publish
	sub := ps.Subscribe("pre-sub")
	defer func() { _ = sub.Close() }()

	_ = ps.Publish("pre-sub", []byte("data"))

	select {
	case received := <-sub.Channel():
		if string(received) != "data" {
			t.Errorf("Expected 'data', got '%s'", string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}
}

func TestPubSubBinaryData(t *testing.T) {
	t.Parallel()
	ps := makeTestPubSub(t)

	sub := ps.Subscribe("binary")
	defer func() { _ = sub.Close() }()

	data := []byte{0x00, 0xFF, 0xAB, 0xCD, 0xEF}
	_ = ps.Publish("binary", data)

	select {
	case received := <-sub.Channel():
		if len(received) != len(data) {
			t.Fatalf("Expected %d bytes, got %d", len(data), len(received))
		}
		for i, b := range data {
			if received[i] != b {
				t.Errorf("Byte %d: expected %x, got %x", i, b, received[i])
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}
}
