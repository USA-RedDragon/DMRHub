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
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

var (
	// ErrUnsupported is returned for a request or subscription that the
	// replica of the hub that holds the plugin's stream does not serve,
	// such as on a hub older than the request. Conn.Has tells beforehand.
	ErrUnsupported = errors.New("the hub does not serve this")
	// ErrLost is returned when the stream that carried a request ended
	// before its answer. The hub may have done the request.
	ErrLost = errors.New("the stream to the hub ended before the answer")
)

// The errors that an *AnswerError matches with errors.Is, one for each
// pluginv1.ErrorCode.
var (
	ErrNotFound    = errors.New("not found")
	ErrInvalid     = errors.New("invalid request")
	ErrRefused     = errors.New("refused")
	ErrFull        = errors.New("the plugin store is full")
	ErrUnavailable = errors.New("the hub cannot do it now")
	ErrNoFormat    = errors.New("the hub knows no text format of the radio")
	ErrRate        = errors.New("too many requests")
)

// maxStoreList is the most keys of one StoreList.
const maxStoreList = 100

func codeError(c pluginv1.ErrorCode) error {
	switch c {
	case pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND:
		return ErrNotFound
	case pluginv1.ErrorCode_ERROR_CODE_INVALID:
		return ErrInvalid
	case pluginv1.ErrorCode_ERROR_CODE_REFUSED:
		return ErrRefused
	case pluginv1.ErrorCode_ERROR_CODE_FULL:
		return ErrFull
	case pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE:
		return ErrUnavailable
	case pluginv1.ErrorCode_ERROR_CODE_NO_FORMAT:
		return ErrNoFormat
	case pluginv1.ErrorCode_ERROR_CODE_RATE:
		return ErrRate
	case pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED:
	}
	return nil
}

// AnswerError is a request that the hub answered with an error.
type AnswerError struct {
	Code pluginv1.ErrorCode
	// Reason is the hub's reason, for a log.
	Reason string
}

func (e *AnswerError) Error() string {
	return fmt.Sprintf("the hub answered %s: %s", e.Code, e.Reason)
}

// Is reports whether target is the error of e.Code, such as ErrNotFound.
func (e *AnswerError) Is(target error) bool {
	err := codeError(e.Code)
	return err != nil && err == target
}

// calls are the requests that wait for their answers, each on the stream
// that carried it.
type calls struct {
	mu     sync.Mutex
	next   uint32
	wait   map[uint32]pending
	closed bool
}

// callStream is a stream that carries requests.
type callStream struct {
	ended bool
}

type pending struct {
	ch chan *pluginv1.Answer
	on *callStream
}

// add returns the id and the channel of the answer of a request on the
// stream on, and false when that stream ended.
func (cs *calls) add(on *callStream) (uint32, chan *pluginv1.Answer, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.closed || on.ended {
		return 0, nil, false
	}
	if cs.wait == nil {
		cs.wait = map[uint32]pending{}
	}
	cs.next++
	ch := make(chan *pluginv1.Answer, 1)
	cs.wait[cs.next] = pending{ch: ch, on: on}
	return cs.next, ch, true
}

// answer hands a to its request.
func (cs *calls) answer(a *pluginv1.Answer) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if p, ok := cs.wait[a.GetId()]; ok {
		delete(cs.wait, a.GetId())
		p.ch <- a
	}
}

func (cs *calls) drop(id uint32) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.wait, id)
}

// fail ends the requests on the stream on, or on every stream when on is
// nil, with ErrLost, and those that come on it later.
func (cs *calls) fail(on *callStream) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if on == nil {
		cs.closed = true
	} else {
		on.ended = true
	}
	for id, p := range cs.wait {
		if on == nil || p.on == on {
			delete(cs.wait, id)
			close(p.ch)
		}
	}
}

// await waits for the answer on ch to the request id.
func (cs *calls) await(ctx context.Context, id uint32, ch chan *pluginv1.Answer) (*pluginv1.Answer, error) {
	select {
	case a, ok := <-ch:
		if !ok {
			return nil, ErrLost
		}
		if a.GetCode() != pluginv1.ErrorCode_ERROR_CODE_UNSPECIFIED {
			return a, &AnswerError{Code: a.GetCode(), Reason: a.GetError()}
		}
		return a, nil
	case <-ctx.Done():
		cs.drop(id)
		return nil, fmt.Errorf("no answer from the hub: %w", ctx.Err())
	}
}

func hasFeature(w *pluginv1.Welcome, f pluginv1.Feature) bool {
	return slices.Contains(w.GetFeatures(), f)
}

// subscribeFeatures returns the features that sub needs.
func subscribeFeatures(sub *pluginv1.Subscribe) []pluginv1.Feature {
	var out []pluginv1.Feature
	if sub.GetHeard() {
		out = append(out, pluginv1.Feature_FEATURE_HEARD)
	}
	if sub.GetTexts() {
		out = append(out, pluginv1.Feature_FEATURE_TEXTS)
	}
	if sub.GetSwitches() {
		out = append(out, pluginv1.Feature_FEATURE_SWITCHES)
	}
	if len(sub.GetAprs()) > 0 {
		out = append(out, pluginv1.Feature_FEATURE_APRS_INBOX)
	}
	return out
}

// served returns the parts of sub that the hub of w serves.
func served(w *pluginv1.Welcome, sub *pluginv1.Subscribe) *pluginv1.Subscribe {
	out := cloneSubscribe(sub)
	if !hasFeature(w, pluginv1.Feature_FEATURE_HEARD) {
		out.Heard = false
	}
	if !hasFeature(w, pluginv1.Feature_FEATURE_TEXTS) {
		out.Texts = false
	}
	if !hasFeature(w, pluginv1.Feature_FEATURE_SWITCHES) {
		out.Switches = false
	}
	if !hasFeature(w, pluginv1.Feature_FEATURE_APRS_INBOX) {
		out.Aprs = nil
	}
	return out
}

// checkText returns ErrInvalid for a string that is not UTF-8, which the
// protocol cannot carry in a string field.
func checkText(values ...string) error {
	for _, v := range values {
		if !utf8.ValidString(v) {
			return fmt.Errorf("%w: not UTF-8", ErrInvalid)
		}
	}
	return nil
}

func featureOf(r *pluginv1.Request) pluginv1.Feature {
	switch r.GetOp().(type) {
	case *pluginv1.Request_StoreGet, *pluginv1.Request_StorePut, *pluginv1.Request_StoreDelete, *pluginv1.Request_StoreList:
		return pluginv1.Feature_FEATURE_STORE
	case *pluginv1.Request_Lookup:
		return pluginv1.Feature_FEATURE_LOOKUP
	case *pluginv1.Request_Repeater:
		return pluginv1.Feature_FEATURE_REPEATER
	case *pluginv1.Request_SendText:
		if r.GetSendText().ReplyTo != nil {
			return pluginv1.Feature_FEATURE_PROMPT
		}
		return pluginv1.Feature_FEATURE_SEND_TEXT
	case *pluginv1.Request_PlayPrompt:
		return pluginv1.Feature_FEATURE_PROMPT
	case *pluginv1.Request_AprsSend:
		return pluginv1.Feature_FEATURE_APRS
	}
	return pluginv1.Feature_FEATURE_UNSPECIFIED
}

// StoreGet returns the value of key in the plugin's store, or ErrNotFound.
// A key is UTF-8.
func (c *Conn) StoreGet(ctx context.Context, key string) ([]byte, error) {
	if err := checkText(key); err != nil {
		return nil, err
	}
	a, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_StoreGet{StoreGet: &pluginv1.StoreGet{Key: key}}})
	if err != nil {
		return nil, err
	}
	return a.GetStoreValue().GetValue(), nil
}

// StorePut sets the value of key in the plugin's store. With a ttl above
// 0, the hub drops the key after it, rounded up to whole seconds.
func (c *Conn) StorePut(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := checkText(key); err != nil {
		return err
	}
	p := &pluginv1.StorePut{Key: key, Value: value}
	if ttl > 0 {
		s := uint32(min((ttl+time.Second-1)/time.Second, 1<<32-1)) //nolint:gosec // bounded
		p.TtlSeconds = &s
	}
	_, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_StorePut{StorePut: p}})
	return err
}

// StoreDelete drops key from the plugin's store.
func (c *Conn) StoreDelete(ctx context.Context, key string) error {
	if err := checkText(key); err != nil {
		return err
	}
	_, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_StoreDelete{StoreDelete: &pluginv1.StoreDelete{Key: key}}})
	return err
}

// StoreList returns at most limit keys of the plugin's store that start
// with prefix and come after after, in byte order, with their values, and
// whether more follow. A limit of 0, or above 100, is 100.
func (c *Conn) StoreList(ctx context.Context, prefix, after string, limit int) ([]*pluginv1.StoreEntry, bool, error) {
	if err := checkText(prefix, after); err != nil {
		return nil, false, err
	}
	n := uint32(0)
	if limit > 0 {
		n = uint32(min(limit, maxStoreList)) //nolint:gosec // 1 to maxStoreList
	}
	a, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_StoreList{StoreList: &pluginv1.StoreList{Prefix: prefix, After: after, Limit: n}}})
	if err != nil {
		return nil, false, err
	}
	return a.GetStoreEntries().GetEntries(), a.GetStoreEntries().GetMore(), nil
}

// LookupRadio returns the user who owns the radio ID radio, or ErrNotFound.
func (c *Conn) LookupRadio(ctx context.Context, radio uint32) (*pluginv1.User, error) {
	return c.lookup(ctx, &pluginv1.Lookup{By: &pluginv1.Lookup_Radio{Radio: radio}})
}

// LookupCallsign returns the user with callsign, in any case, or
// ErrNotFound.
func (c *Conn) LookupCallsign(ctx context.Context, callsign string) (*pluginv1.User, error) {
	if err := checkText(callsign); err != nil {
		return nil, err
	}
	return c.lookup(ctx, &pluginv1.Lookup{By: &pluginv1.Lookup_Callsign{Callsign: callsign}})
}

func (c *Conn) lookup(ctx context.Context, l *pluginv1.Lookup) (*pluginv1.User, error) {
	a, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_Lookup{Lookup: l}})
	if err != nil {
		return nil, err
	}
	return a.GetUser(), nil
}

// Repeater returns the talkgroups of the repeater or hotspot id, such as
// the Repeater of a burst, or ErrNotFound.
func (c *Conn) Repeater(ctx context.Context, id uint32) (*pluginv1.RepeaterInfo, error) {
	a, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_Repeater{Repeater: &pluginv1.RepeaterQuery{Repeater: id}}})
	if err != nil {
		return nil, err
	}
	return a.GetRepeater(), nil
}

// SendText asks the hub to send the text body from source, one of the
// plugin's IDs, to radio, and returns the name of the format the hub sent
// it in. The hub sends a text only in the format of a text that the radio
// itself sent, because a format that a radio does not send can lock it up:
// it returns ErrNoFormat, and sends nothing, when it knows none. To answer
// a text, send to its source: the hub then answers in the format of that
// text.
func (c *Conn) SendText(ctx context.Context, source, radio uint32, body string) (string, error) {
	if err := checkText(body); err != nil {
		return "", err
	}
	a, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_SendText{SendText: &pluginv1.SendText{Source: source, Radio: radio, Body: body}}})
	if err != nil {
		return "", err
	}
	return a.GetTextSent().GetFormat(), nil
}

// ReplyText asks the hub to answer text, a Text to one of the plugin's
// IDs, with body from source, and returns the name of the format the hub
// sent it in. The hub builds the answer in the exact format of text, not of
// another text of the radio, and sends it through the repeater and
// timeslot that text came on. It returns ErrNoFormat, and the hub sends
// nothing, when it cannot build that format again exactly or does not
// trust the repeater of text, and ErrUnsupported from a hub without
// FEATURE_PROMPT, which would answer in another format.
func (c *Conn) ReplyText(ctx context.Context, source uint32, text *pluginv1.Text, body string) (string, error) {
	if err := checkText(body); err != nil {
		return "", err
	}
	id := text.GetId()
	a, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_SendText{SendText: &pluginv1.SendText{
		Source: source, Radio: text.GetSource(), Body: body, ReplyTo: &id,
	}}})
	if err != nil {
		return "", err
	}
	return a.GetTextSent().GetFormat(), nil
}

// PlayPrompt asks the hub to play p, spoken prompts, to a radio as a
// private voice call from one of the plugin's IDs, as PlayPrompt in the
// protocol describes. It returns once the hub queued the call.
func (c *Conn) PlayPrompt(ctx context.Context, p *pluginv1.PlayPrompt) error {
	if err := checkText(p.GetClips()...); err != nil {
		return err
	}
	_, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_PlayPrompt{PlayPrompt: p}})
	return err
}

// SendAPRS asks the hub to send the APRS message m on its APRS-IS login.
// The source of m is the address of a user who turned a switch of the
// plugin on. The hub sends at most 30 messages a minute for a plugin. An
// answer with no error means that the hub handed the message to its
// APRS-IS login, not that the addressee got it.
func (c *Conn) SendAPRS(ctx context.Context, m *pluginv1.APRSSend) error {
	if err := checkText(m.GetSource(), m.GetAddressee(), m.GetText(), m.GetNumber()); err != nil {
		return err
	}
	_, err := c.Request(ctx, &pluginv1.Request{Op: &pluginv1.Request_AprsSend{AprsSend: m}})
	return err
}
