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

// Package mailbox is a DMRHub plugin that keeps text messages for
// callsigns. A radio texts the mailbox ID with a callsign and a message to
// leave one, and INBOX, GET n, DELETE n or DELETE ALL to read and remove
// the messages for its owner's callsign. Each answer goes in the exact
// format of the text that it answers. When one of the recipient's
// radios is next heard, the mailbox texts it a short notice, and it emails
// a recipient who turned its email switch on.
package mailbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

const (
	// Keep is how long the mailbox keeps a message.
	Keep = 30 * 24 * time.Hour
	// MaxText is the longest message that the mailbox keeps, in
	// characters.
	MaxText = 120
	// MaxMessages is the most messages that the mailbox keeps for one
	// callsign.
	MaxMessages = 20
	// maxReply is the longest text that the mailbox sends, in characters.
	maxReply = 160
	// previewWords is how many words of each message INBOX shows.
	previewWords = 3
	// noticeRetry is how long the mailbox waits before it tries again to
	// text a notice to a radio that it could not reach.
	noticeRetry = 10 * time.Minute
)

// Hub is what the mailbox asks of the hub. A *plugin.Conn is a Hub.
type Hub interface {
	StoreGet(ctx context.Context, key string) ([]byte, error)
	StorePut(ctx context.Context, key string, value []byte, ttl time.Duration) error
	StoreDelete(ctx context.Context, key string) error
	StoreList(ctx context.Context, prefix, after string, limit int) ([]*pluginv1.StoreEntry, bool, error)
	LookupRadio(ctx context.Context, radio uint32) (*pluginv1.User, error)
	LookupCallsign(ctx context.Context, callsign string) (*pluginv1.User, error)
	SendText(ctx context.Context, source, radio uint32, body string) (string, error)
	ReplyText(ctx context.Context, source uint32, text *pluginv1.Text, body string) (string, error)
	SendEmail(ctx context.Context, userID uint32, switchKey, subject, body string) error
}

// Mailbox keeps the messages in the store of its hub. It handles one
// message of the hub at a time.
type Mailbox struct {
	hub     Hub
	id      uint32
	pending map[uint32]string
	tried   map[uint32]time.Time
}

// notice is the count of new messages for a callsign since its last
// notice, whether the mailbox emailed the user about them, and the radios
// of the user, which get the notice.
type notice struct {
	count   int
	emailed bool
	radios  []uint32
}

// message is a message in the mailbox.
type message struct {
	key  string
	at   time.Time
	from string
	text string
}

// New returns a Mailbox that keeps its messages with hub and answers from
// id.
func New(hub Hub, id uint32) *Mailbox {
	return &Mailbox{hub: hub, id: id, pending: map[uint32]string{}, tried: map[uint32]time.Time{}}
}

// Serve runs a Mailbox on conn until it ends. Give it to plugin.Run.
func Serve(ctx context.Context, conn *plugin.Conn) error {
	return serve(ctx, conn, func() {})
}

// serve runs a Mailbox on conn, and calls subscribed once the hub sends
// the texts to it: the hub sends the UserSwitches after it follows the
// texts.
func serve(ctx context.Context, conn *plugin.Conn, subscribed func()) error {
	ranges := conn.Ranges()
	if len(ranges) == 0 {
		return errNoIDs
	}
	m := New(conn, ranges[0].GetFirst())
	if conn.Has(pluginv1.Feature_FEATURE_STORE) {
		if err := m.Load(ctx); err != nil {
			return err
		}
	}
	sub := &pluginv1.Subscribe{
		Texts:    conn.Has(pluginv1.Feature_FEATURE_TEXTS),
		Heard:    conn.Has(pluginv1.Feature_FEATURE_HEARD),
		Switches: conn.Has(pluginv1.Feature_FEATURE_SWITCHES),
	}
	if err := conn.Subscribe(sub); err != nil {
		return fmt.Errorf("cannot subscribe to texts: %w", err)
	}
	for {
		r, err := conn.Recv()
		if err != nil {
			return fmt.Errorf("cannot receive: %w", err)
		}
		switch msg := r.GetMessage().(type) {
		case *pluginv1.ConnectResponse_Text:
			m.Text(ctx, msg.Text)
		case *pluginv1.ConnectResponse_Heard:
			m.Heard(ctx, msg.Heard)
		case *pluginv1.ConnectResponse_UserSwitches:
			subscribed()
		case *pluginv1.ConnectResponse_Ranges:
			if r := msg.Ranges.GetRanges(); len(r) > 0 {
				m.id = r[0].GetFirst()
			}
		default:
		}
	}
}

var errNoIDs = errors.New("the plugin has no IDs")

func messagePrefix(call string) string {
	return "m/" + call + "/"
}

func noticeKey(call string) string {
	return "n/" + call
}

// Load reads which radios wait for a notice from the store.
func (m *Mailbox) Load(ctx context.Context) error {
	after := ""
	for {
		entries, more, err := m.hub.StoreList(ctx, "n/", after, 0)
		if err != nil {
			return fmt.Errorf("cannot read the notices: %w", err)
		}
		for _, e := range entries {
			n := parseNotice(e.GetValue())
			for _, r := range n.radios {
				m.pending[r] = strings.TrimPrefix(e.GetKey(), "n/")
			}
			after = e.GetKey()
		}
		if !more || len(entries) == 0 {
			return nil
		}
	}
}

func parseNotice(b []byte) notice {
	var n notice
	parts := strings.SplitN(string(b), "\n", 3)
	if len(parts) != 3 {
		return n
	}
	n.count, _ = strconv.Atoi(parts[0])
	n.emailed = parts[1] == "1"
	for _, f := range strings.Split(parts[2], ",") {
		if r, err := strconv.ParseUint(f, 10, 32); err == nil {
			n.radios = append(n.radios, uint32(r))
		}
	}
	return n
}

func (n notice) encode() []byte {
	emailed := "0"
	if n.emailed {
		emailed = "1"
	}
	radios := make([]string, len(n.radios))
	for i, r := range n.radios {
		radios[i] = strconv.FormatUint(uint64(r), 10)
	}
	return []byte(strconv.Itoa(n.count) + "\n" + emailed + "\n" + strings.Join(radios, ","))
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + one + "s"
}

// fit cuts s to maxReply characters.
func fit(s string) string {
	if utf8.RuneCountInString(s) <= maxReply {
		return s
	}
	return string([]rune(s)[:maxReply])
}

// reply answers t with body, from the ID that t went to, in the format of
// t: a radio can lock up on a format that it does not send itself.
func (m *Mailbox) reply(ctx context.Context, t *pluginv1.Text, body string) {
	if _, err := m.hub.ReplyText(ctx, t.GetDestination(), t, fit(body)); err != nil {
		slog.Warn("Mailbox: cannot answer a text", "radio", t.GetSource(), "error", err)
	}
}

func (m *Mailbox) unavailable(ctx context.Context, t *pluginv1.Text, what string, err error) {
	slog.Error("Mailbox: cannot "+what, "error", err)
	m.reply(ctx, t, "The mailbox cannot answer now. Try again later.")
}

func (m *Mailbox) help(ctx context.Context, t *pluginv1.Text) {
	m.reply(ctx, t, "Text CALLSIGN and a message to leave one, or INBOX, GET 1, DELETE 1 or DELETE ALL.")
}

// Text handles a text message to one of the mailbox's IDs.
func (m *Mailbox) Text(ctx context.Context, t *pluginv1.Text) {
	if t.GetGroup() {
		return
	}
	fields := strings.Fields(t.GetBody())
	if len(fields) == 0 {
		m.help(ctx, t)
		return
	}
	switch word := strings.ToUpper(fields[0]); {
	case word == "INBOX" && len(fields) == 1:
		m.inbox(ctx, t)
	case word == "GET" && len(fields) == 2:
		m.get(ctx, t, fields[1])
	case word == "DELETE" && len(fields) == 2:
		m.delete(ctx, t, fields[1])
	case word == "INBOX", word == "GET", word == "DELETE", word == "HELP", len(fields) == 1:
		m.help(ctx, t)
	default:
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t.GetBody()), fields[0]))
		m.leave(ctx, t, fields[0], text)
	}
}

// owner returns the callsign whose messages t asks for, or false after it
// answered t.
func (m *Mailbox) owner(ctx context.Context, t *pluginv1.Text) (string, bool) {
	u, err := m.hub.LookupRadio(ctx, t.GetSource())
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		m.reply(ctx, t, fmt.Sprintf("Radio %d has no user on this network, so it has no mailbox.", t.GetSource()))
		return "", false
	case err != nil:
		m.unavailable(ctx, t, "look up a radio", err)
		return "", false
	}
	return strings.ToUpper(u.GetCallsign()), true
}

func (m *Mailbox) messages(ctx context.Context, call string) ([]message, error) {
	var out []message
	after := ""
	for {
		entries, more, err := m.hub.StoreList(ctx, messagePrefix(call), after, 0)
		if err != nil {
			return nil, fmt.Errorf("cannot list the messages of %s: %w", call, err)
		}
		for _, e := range entries {
			out = append(out, parseMessage(e))
			after = e.GetKey()
		}
		if !more || len(entries) == 0 {
			return out, nil
		}
	}
}

func parseMessage(e *pluginv1.StoreEntry) message {
	msg := message{key: e.GetKey()}
	rest := e.GetKey()[strings.LastIndex(e.GetKey(), "/")+1:]
	stamp, _, _ := strings.Cut(rest, "-")
	if ms, err := strconv.ParseInt(stamp, 10, 64); err == nil {
		msg.at = time.UnixMilli(ms).UTC()
	}
	msg.from, msg.text, _ = strings.Cut(string(e.GetValue()), "\n")
	return msg
}

func (m *Mailbox) leave(ctx context.Context, t *pluginv1.Text, to, text string) {
	if text == "" {
		m.help(ctx, t)
		return
	}
	if utf8.RuneCountInString(text) > MaxText {
		m.reply(ctx, t, fmt.Sprintf("Too long: a message has at most %d characters.", MaxText))
		return
	}
	user, err := m.hub.LookupCallsign(ctx, to)
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		m.reply(ctx, t, fmt.Sprintf("No user has callsign %s on this network.", strings.ToUpper(to)))
		return
	case err != nil:
		m.unavailable(ctx, t, "look up a callsign", err)
		return
	}
	call := strings.ToUpper(user.GetCallsign())
	have, err := m.messages(ctx, call)
	if err != nil {
		m.unavailable(ctx, t, "list messages", err)
		return
	}
	if len(have) >= MaxMessages {
		m.reply(ctx, t, fmt.Sprintf("The mailbox of %s is full.", call))
		return
	}
	from := strconv.FormatUint(uint64(t.GetSource()), 10)
	if sender, err := m.hub.LookupRadio(ctx, t.GetSource()); err == nil {
		from = strings.ToUpper(sender.GetCallsign())
	}
	at := time.UnixMilli(t.GetAt())
	if t.GetAt() == 0 {
		at = time.Now()
	}
	key := fmt.Sprintf("%s%013d-%020d", messagePrefix(call), at.UnixMilli(), t.GetId())
	err = m.hub.StorePut(ctx, key, []byte(from+"\n"+text), Keep)
	switch {
	case errors.Is(err, plugin.ErrFull):
		m.reply(ctx, t, "The mailbox is full. Try again later.")
		return
	case err != nil:
		m.unavailable(ctx, t, "keep a message", err)
		return
	}
	m.note(ctx, user, call, from)
	m.reply(ctx, t, "Saved for "+call+".")
}

// note counts a new message for user in the notice of call, and emails
// the user when its email switch is on and no email went out since the
// last notice.
func (m *Mailbox) note(ctx context.Context, user *pluginv1.User, call, from string) {
	var n notice
	if b, err := m.hub.StoreGet(ctx, noticeKey(call)); err == nil {
		n = parseNotice(b)
	}
	n.count++
	n.radios = user.GetRadios()
	if !n.emailed && emailOn(user) {
		subject := "New message for " + call
		body := fmt.Sprintf("%s left a message for %s in the mailbox.\n\nText INBOX to %d from your radio to read it.\n", from, call, m.id)
		if err := m.hub.SendEmail(ctx, user.GetUserId(), EmailSwitch, subject, body); err != nil {
			slog.Warn("Mailbox: cannot email a notice", "callsign", call, "error", err)
		} else {
			n.emailed = true
		}
	}
	if err := m.hub.StorePut(ctx, noticeKey(call), n.encode(), Keep); err != nil {
		slog.Error("Mailbox: cannot keep a notice", "callsign", call, "error", err)
		return
	}
	for _, r := range n.radios {
		m.pending[r] = call
		delete(m.tried, r)
	}
}

func emailOn(user *pluginv1.User) bool {
	for _, s := range user.GetSwitches() {
		if s.GetKey() == EmailSwitch && s.GetOn() {
			return true
		}
	}
	return false
}

// read drops the notice of call, whose user reads the messages.
func (m *Mailbox) read(ctx context.Context, call string) {
	if err := m.hub.StoreDelete(ctx, noticeKey(call)); err != nil {
		slog.Warn("Mailbox: cannot drop a notice", "callsign", call, "error", err)
		return
	}
	for r, c := range m.pending {
		if c == call {
			delete(m.pending, r)
			delete(m.tried, r)
		}
	}
}

func preview(text string) string {
	words := strings.Fields(text)
	if len(words) > previewWords {
		return strings.Join(words[:previewWords], " ") + "..."
	}
	return strings.Join(words, " ")
}

func (m *Mailbox) inbox(ctx context.Context, t *pluginv1.Text) {
	call, ok := m.owner(ctx, t)
	if !ok {
		return
	}
	msgs, err := m.messages(ctx, call)
	if err != nil {
		m.unavailable(ctx, t, "list messages", err)
		return
	}
	m.read(ctx, call)
	if len(msgs) == 0 {
		m.reply(ctx, t, "No messages for "+call+".")
		return
	}
	out := plural(len(msgs), "message") + ":"
	for i, msg := range msgs {
		item := fmt.Sprintf(" %d %s %s", i+1, msg.from, preview(msg.text))
		more := ""
		if i+1 < len(msgs) {
			more = fmt.Sprintf(" +%d more", len(msgs)-i-1)
		}
		if utf8.RuneCountInString(out+item+more) > maxReply {
			out += fmt.Sprintf(" +%d more", len(msgs)-i)
			break
		}
		out += item
	}
	m.reply(ctx, t, out)
}

// pick returns the message n of the messages of t's owner, or false after
// it answered t.
func (m *Mailbox) pick(ctx context.Context, t *pluginv1.Text, n string) ([]message, int, bool) {
	call, ok := m.owner(ctx, t)
	if !ok {
		return nil, 0, false
	}
	msgs, err := m.messages(ctx, call)
	if err != nil {
		m.unavailable(ctx, t, "list messages", err)
		return nil, 0, false
	}
	m.read(ctx, call)
	if strings.EqualFold(n, "ALL") {
		return msgs, -1, true
	}
	i, err := strconv.Atoi(n)
	if err != nil || i < 1 || i > len(msgs) {
		m.reply(ctx, t, fmt.Sprintf("No message %s. You have %s.", n, plural(len(msgs), "message")))
		return nil, 0, false
	}
	return msgs, i - 1, true
}

func (m *Mailbox) get(ctx context.Context, t *pluginv1.Text, n string) {
	if strings.EqualFold(n, "ALL") {
		m.help(ctx, t)
		return
	}
	msgs, i, ok := m.pick(ctx, t, n)
	if !ok {
		return
	}
	msg := msgs[i]
	m.reply(ctx, t, fmt.Sprintf("%d/%d %s %sZ: %s", i+1, len(msgs), msg.from, msg.at.Format("01-02 15:04"), msg.text))
}

func (m *Mailbox) delete(ctx context.Context, t *pluginv1.Text, n string) {
	msgs, i, ok := m.pick(ctx, t, n)
	if !ok {
		return
	}
	drop := msgs
	if i >= 0 {
		drop = msgs[i : i+1]
	}
	for _, msg := range drop {
		if err := m.hub.StoreDelete(ctx, msg.key); err != nil {
			m.unavailable(ctx, t, "delete a message", err)
			return
		}
	}
	if i < 0 {
		m.reply(ctx, t, "Deleted all "+plural(len(msgs), "message")+".")
		return
	}
	m.reply(ctx, t, fmt.Sprintf("Deleted %d. %s left.", i+1, plural(len(msgs)-1, "message")))
}

// Heard texts a notice to h's radio when messages wait for its user.
func (m *Mailbox) Heard(ctx context.Context, h *pluginv1.Heard) {
	radio := h.GetRadio()
	call, ok := m.pending[radio]
	if !ok {
		return
	}
	now := time.Now()
	if at, ok := m.tried[radio]; ok && now.Sub(at) < noticeRetry {
		return
	}
	m.tried[radio] = now
	b, err := m.hub.StoreGet(ctx, noticeKey(call))
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		delete(m.pending, radio)
		delete(m.tried, radio)
		return
	case err != nil:
		slog.Warn("Mailbox: cannot read a notice", "callsign", call, "error", err)
		return
	}
	n := parseNotice(b)
	body := fmt.Sprintf("%s for %s. Text INBOX to %d.", plural(max(n.count, 1), "new message"), call, m.id)
	if _, err := m.hub.SendText(ctx, m.id, radio, body); err != nil {
		slog.Info("Mailbox: cannot text a notice yet", "radio", radio, "error", err)
		return
	}
	m.read(ctx, call)
}
