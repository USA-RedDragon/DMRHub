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
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"gorm.io/gorm"
)

const (
	// maxRequests is how many requests of a plugin wait for the hub before
	// it answers the next ones with ERROR_CODE_RATE.
	maxRequests = 64
	// maxTextBody is the longest text that a plugin sends, in characters.
	maxTextBody = 500
	// maxPromptClips is the most clips of one PlayPrompt.
	maxPromptClips = 64
	// aprsPerMinute is how many APRS messages a plugin sends in a minute.
	aprsPerMinute = 30
	// aprsLimitWait is how long the count of the APRS messages of a
	// plugin waits for the KV store.
	aprsLimitWait = 5 * time.Second
	// emailsPerMinute is how many emails a plugin sends in a minute.
	emailsPerMinute = 30
	// maxEmailSubject and maxEmailBody are the longest subject and body
	// of an email of a plugin, in characters.
	maxEmailSubject = 120
	maxEmailBody    = 4000
)

// The errors of Texts.SendText.
var (
	// ErrNoFormat is returned when the hub knows no text format that the
	// radio sends.
	ErrNoFormat = errors.New("the hub knows no text format that the radio sends")
	// ErrTextDoesNotFit is returned for a text that the format of the
	// radio cannot carry.
	ErrTextDoesNotFit = errors.New("the text does not fit the text format of the radio")
	// ErrRadioRefused is returned when the radio refused the text.
	ErrRadioRefused = errors.New("the radio refused the text")
	// ErrNoAnswer is returned when the radio did not answer a text that
	// asks for an answer.
	ErrNoAnswer = errors.New("the radio did not answer the text")
)

// Texts sends the text messages of plugins.
type Texts interface {
	// SendText sends body from source to radio, in the format of a text
	// that radio sent, and returns the name of the format. It returns
	// ErrNoFormat, and sends nothing, when the hub knows no such text, and
	// ErrTextDoesNotFit, ErrRadioRefused or ErrNoAnswer.
	SendText(ctx context.Context, source, radio uint, body string) (string, error)
	// ReplyText sends body from source to radio in the exact format of the
	// text id that radio sent to source, through its repeater and timeslot,
	// and returns the name of the format. It returns ErrNoFormat, and sends
	// nothing, when the hub cannot or may not mirror that text, and the
	// errors of SendText.
	ReplyText(ctx context.Context, source uint, id uint64, radio uint, body string) (string, error)
}

// The errors of Prompts.PlayPrompt.
var (
	// ErrRadioNotFound is returned when the hub does not know where to
	// reach the radio.
	ErrRadioNotFound = errors.New("the hub does not know where the radio is")
	// ErrUnknownClip is returned for a clip that the hub does not have.
	ErrUnknownClip = errors.New("the hub has no such clip")
)

// Prompts plays the spoken prompts of plugins.
type Prompts interface {
	// PlayPrompt plays clips, one after another, from source to radio on
	// the timeslot (true for timeslot 2), as the hub plays its own
	// prompts, at most once for the stream of the radio when stream is not
	// 0. It returns ErrRadioNotFound or ErrUnknownClip.
	PlayPrompt(ctx context.Context, source, radio uint, slot bool, clips []string, stream uint) error
}

// WithPrompts makes the service play the prompts of plugins with p.
// With it and WithTexts, the hub offers FEATURE_PROMPT.
func WithPrompts(p Prompts) ServiceOption {
	return func(s *Service) { s.prompts = p }
}

// WithTexts makes the service send the text messages of plugins with t.
// Without it, the hub does not offer FEATURE_SEND_TEXT.
func WithTexts(t Texts) ServiceOption {
	return func(s *Service) { s.texts = t }
}

// APRS sends the APRS messages of plugins.
type APRS struct {
	// Send hands a message to the replica that holds the APRS-IS login.
	Send func(aprs.Message) error
	// On reports whether the hub has an APRS-IS login.
	On func() bool
	// Limits keeps how many messages each plugin sent in the last minute,
	// for every replica.
	Limits kv.KV
}

// WithAPRS makes the service send the APRS messages of plugins with a.
// Without it, the hub does not offer FEATURE_APRS.
func WithAPRS(a APRS) ServiceOption {
	return func(s *Service) { s.aprs = &a }
}

// Email sends the emails of plugins.
type Email struct {
	// Send sends a plain text email.
	Send func(to, subject, body string) error
	// On reports whether the hub has SMTP set up.
	On func() bool
	// Limits keeps how many emails each plugin sent in the last minute,
	// for every replica.
	Limits kv.KV
}

// WithEmail makes the service send the emails of plugins with e. Without
// it, the hub does not offer FEATURE_EMAIL.
func WithEmail(e Email) ServiceOption {
	return func(s *Service) { s.email = &e }
}

// WithAPRSInbox makes the hub offer FEATURE_APRS_INBOX: a replica
// publishes the APRS messages from APRS-IS with PublishAPRS.
func WithAPRSInbox() ServiceOption {
	return func(s *Service) { s.aprsInbox = true }
}

// features returns what the service serves besides bursts, tracking,
// positions and the network.
func (s *Service) features() []pluginv1.Feature {
	f := []pluginv1.Feature{
		pluginv1.Feature_FEATURE_STORE, pluginv1.Feature_FEATURE_LOOKUP, pluginv1.Feature_FEATURE_REPEATER,
		pluginv1.Feature_FEATURE_HEARD, pluginv1.Feature_FEATURE_TEXTS, pluginv1.Feature_FEATURE_SWITCHES,
	}
	if s.texts != nil {
		f = append(f, pluginv1.Feature_FEATURE_SEND_TEXT)
	}
	if s.aprs != nil {
		f = append(f, pluginv1.Feature_FEATURE_APRS)
	}
	if s.texts != nil && s.prompts != nil {
		f = append(f, pluginv1.Feature_FEATURE_PROMPT)
	}
	if s.aprsInbox {
		f = append(f, pluginv1.Feature_FEATURE_APRS_INBOX)
	}
	if s.email != nil {
		f = append(f, pluginv1.Feature_FEATURE_EMAIL)
	}
	return f
}

// request is a request of a plugin with its IDs when it came.
type request struct {
	r      *pluginv1.Request
	ranges []models.PluginRange
}

// requester answers the requests of one plugin's stream, in order.
type requester struct {
	s     *Service
	p     *models.Plugin
	send  func(*pluginv1.ConnectResponse)
	queue chan request
}

// newRequester returns the requester of the plugin p, which answers with
// send. send must not drop an answer: it waits until the stream takes it
// or ends.
func newRequester(s *Service, p *models.Plugin, send func(*pluginv1.ConnectResponse)) *requester {
	return &requester{s: s, p: p, send: send, queue: make(chan request, maxRequests)}
}

// take queues r, or answers it with ERROR_CODE_RATE when the queue is full.
func (q *requester) take(r *pluginv1.Request, ranges []models.PluginRange) {
	select {
	case q.queue <- request{r: r, ranges: ranges}:
	default:
		q.answer(r, fail(pluginv1.ErrorCode_ERROR_CODE_RATE, "too many requests at once"))
	}
}

// errAPRSRate stops the update of the APRS limit of a plugin that sent
// aprsPerMinute messages in the last minute.
var errAPRSRate = errors.New("at most 30 APRS messages a minute")

// errEmailRate stops the update of the email limit of a plugin that sent
// emailsPerMinute emails in the last minute.
var errEmailRate = errors.New("at most 30 emails a minute")

func aprsLimitKey(plugin uint) string {
	return "dmrhub:plugins:aprs:" + strconv.FormatUint(uint64(plugin), 10)
}

func emailLimitKey(plugin uint) string {
	return "dmrhub:plugins:email:" + strconv.FormatUint(uint64(plugin), 10)
}

// countAPRS notes a message of the plugin at now in its limit, which every
// replica shares, and returns errAPRSRate when the plugin sent
// aprsPerMinute messages in the minute before now.
func (q *requester) countAPRS(ctx context.Context, now time.Time) error {
	return countInMinute(ctx, q.s.aprs.Limits, aprsLimitKey(q.p.ID), now, aprsPerMinute, errAPRSRate)
}

// countEmail notes an email of the plugin at now in its limit, as
// countAPRS does.
func (q *requester) countEmail(ctx context.Context, now time.Time) error {
	return countInMinute(ctx, q.s.email.Limits, emailLimitKey(q.p.ID), now, emailsPerMinute, errEmailRate)
}

// countInMinute notes now in the times at key and returns full when limit
// of them are in the minute before now.
func countInMinute(ctx context.Context, limits kv.KV, key string, now time.Time, limit int, full error) error {
	ms := now.UnixMilli()
	ctx, cancel := context.WithTimeout(ctx, aprsLimitWait)
	defer cancel()
	err := limits.Update(ctx, key, time.Minute, func(old []byte) ([]byte, error) {
		out := make([]byte, 0, len(old)+8)
		for i := 0; i+8 <= len(old); i += 8 {
			if at := int64(binary.BigEndian.Uint64(old[i:])); ms-at < time.Minute.Milliseconds() { //nolint:gosec // written from an int64
				out = append(out, old[i:i+8]...)
			}
		}
		if len(out)/8 >= limit {
			return nil, full
		}
		return binary.BigEndian.AppendUint64(out, uint64(ms)), nil //nolint:gosec // a time after 1970
	})
	if err != nil {
		return fmt.Errorf("counting at %s: %w", key, err)
	}
	return nil
}

func (q *requester) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-q.queue:
			q.answer(r.r, q.serve(ctx, r))
		}
	}
}

func (q *requester) answer(r *pluginv1.Request, a *pluginv1.Answer) {
	a.Id = r.GetId()
	q.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Answer{Answer: a}})
}

func fail(code pluginv1.ErrorCode, why string) *pluginv1.Answer {
	return &pluginv1.Answer{Code: code, Error: why}
}

func (q *requester) unavailable(what string, err error) *pluginv1.Answer {
	slog.Error("Plugins: cannot "+what, "plugin", q.p.Name, "error", err)
	return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, "the hub cannot "+what+" now")
}

func (q *requester) serve(ctx context.Context, r request) *pluginv1.Answer {
	now := q.s.clock.Now()
	db := q.s.db.WithContext(ctx)
	switch op := r.r.GetOp().(type) {
	case *pluginv1.Request_StoreGet:
		return q.storeGet(db, op.StoreGet, now)
	case *pluginv1.Request_StorePut:
		return q.storePut(db, op.StorePut, now)
	case *pluginv1.Request_StoreDelete:
		if err := models.PluginStoreDelete(db, q.p.ID, op.StoreDelete.GetKey()); err != nil {
			return q.storeError(err)
		}
		return &pluginv1.Answer{}
	case *pluginv1.Request_StoreList:
		return q.storeList(db, op.StoreList, now)
	case *pluginv1.Request_Lookup:
		return q.lookup(db, op.Lookup)
	case *pluginv1.Request_Repeater:
		return q.repeater(db, op.Repeater.GetRepeater())
	case *pluginv1.Request_SendText:
		return q.sendText(ctx, op.SendText, r.ranges)
	case *pluginv1.Request_AprsSend:
		return q.sendAPRS(ctx, op.AprsSend, now)
	case *pluginv1.Request_PlayPrompt:
		return q.playPrompt(ctx, op.PlayPrompt, r.ranges)
	case *pluginv1.Request_SendEmail:
		return q.sendEmail(ctx, op.SendEmail, now)
	}
	return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the hub does not know this request")
}

func (q *requester) storeError(err error) *pluginv1.Answer {
	switch {
	case errors.Is(err, models.ErrPluginStoreKey), errors.Is(err, models.ErrPluginStoreValue):
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, err.Error())
	case errors.Is(err, models.ErrPluginStoreFull):
		return fail(pluginv1.ErrorCode_ERROR_CODE_FULL, err.Error())
	}
	return q.unavailable("use the plugin store", err)
}

func expiresOf(at *time.Time) *int64 {
	if at == nil {
		return nil
	}
	ms := at.UnixMilli()
	return &ms
}

func (q *requester) storeGet(db *gorm.DB, g *pluginv1.StoreGet, now time.Time) *pluginv1.Answer {
	e, ok, err := models.PluginStoreGet(db, q.p.ID, g.GetKey(), now)
	if err != nil {
		return q.storeError(err)
	}
	if !ok {
		return fail(pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, "no such key")
	}
	return &pluginv1.Answer{Result: &pluginv1.Answer_StoreValue{StoreValue: &pluginv1.StoreValue{Value: e.Value, ExpiresAt: expiresOf(e.ExpiresAt)}}}
}

func (q *requester) storePut(db *gorm.DB, p *pluginv1.StorePut, now time.Time) *pluginv1.Answer {
	var expires *time.Time
	if p.TtlSeconds != nil {
		at := now.Add(time.Duration(p.GetTtlSeconds()) * time.Second)
		expires = &at
	}
	if err := models.PluginStorePut(db, q.p.ID, p.GetKey(), p.GetValue(), expires, now); err != nil {
		return q.storeError(err)
	}
	return &pluginv1.Answer{}
}

func (q *requester) storeList(db *gorm.DB, l *pluginv1.StoreList, now time.Time) *pluginv1.Answer {
	if l.GetLimit() > models.MaxPluginStoreList {
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "a list returns at most 100 keys")
	}
	found, more, err := models.PluginStoreList(db, q.p.ID, l.GetPrefix(), l.GetAfter(), int(l.GetLimit()), now)
	if err != nil {
		return q.storeError(err)
	}
	out := &pluginv1.StoreEntries{More: more, Entries: make([]*pluginv1.StoreEntry, 0, len(found))}
	for i := range found {
		out.Entries = append(out.Entries, &pluginv1.StoreEntry{Key: found[i].Key(), Value: found[i].Value, ExpiresAt: expiresOf(found[i].ExpiresAt)})
	}
	return &pluginv1.Answer{Result: &pluginv1.Answer_StoreEntries{StoreEntries: out}}
}

// LookupUser returns the user of the radio ID radio, or of callsign when
// radio is 0, as plugin sees the user, and false when no approved user who
// is not suspended has it.
func LookupUser(db *gorm.DB, plugin uint, radio uint, callsign string) (*pluginv1.User, bool, error) {
	var user models.User
	switch {
	case radio != 0:
		owner, ok, err := models.RadioIDOwner(db, radio)
		if err != nil || !ok {
			return nil, false, err //nolint:wrapcheck // RadioIDOwner wraps its errors
		}
		var users []models.User
		if err := db.Where("id = ?", owner).Limit(1).Find(&users).Error; err != nil {
			return nil, false, err //nolint:wrapcheck // the database's error
		}
		if len(users) == 0 {
			return nil, false, nil
		}
		user = users[0]
	case callsign != "":
		u, ok, err := models.FindUserByCallsign(db, callsign)
		if err != nil || !ok {
			return nil, false, err //nolint:wrapcheck // FindUserByCallsign wraps its errors
		}
		user = u
	default:
		return nil, false, nil
	}
	if !user.Approved || user.Suspended {
		return nil, false, nil
	}
	ids, err := models.ListUserRadioIDs(db, user.ID)
	if err != nil {
		return nil, false, err //nolint:wrapcheck // ListUserRadioIDs wraps its errors
	}
	out := &pluginv1.User{UserId: uint32(user.ID), Callsign: user.Callsign, Radios: []uint32{uint32(user.ID)}} //nolint:gosec // DMR IDs are 24 bits
	for _, id := range ids {
		out.Radios = append(out.Radios, uint32(id.RadioID)) //nolint:gosec // DMR IDs are 24 bits
	}
	switches, err := models.UserPluginSwitches(db, plugin, &user)
	if err != nil {
		return nil, false, err //nolint:wrapcheck // UserPluginSwitches wraps its errors
	}
	for i := range switches {
		out.Switches = append(out.Switches, userSwitchOf(&switches[i]))
	}
	return out, true, nil
}

func userSwitchOf(u *models.UserPluginSwitch) *pluginv1.UserSwitch {
	return &pluginv1.UserSwitch{UserId: uint32(u.UserID), Callsign: u.Callsign, Key: u.Spec.Key, On: u.On, Choice: u.Choice} //nolint:gosec // DMR IDs are 24 bits
}

func (q *requester) lookup(db *gorm.DB, l *pluginv1.Lookup) *pluginv1.Answer {
	if l.GetRadio() == 0 && l.GetCallsign() == "" {
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "give a radio ID or a callsign")
	}
	u, ok, err := LookupUser(db, q.p.ID, uint(l.GetRadio()), l.GetCallsign())
	if err != nil {
		return q.unavailable("look up a user", err)
	}
	if !ok {
		return fail(pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, "no such user")
	}
	return &pluginv1.Answer{Result: &pluginv1.Answer_User{User: u}}
}

func talkgroupIDs(tgs []models.Talkgroup) []uint32 {
	out := make([]uint32, 0, len(tgs))
	for _, tg := range tgs {
		out = append(out, uint32(tg.ID)) //nolint:gosec // talkgroup IDs are 24 bits
	}
	slices.Sort(out)
	return out
}

func dynamicOf(id *uint) *uint32 {
	if id == nil || *id == 0 {
		return nil
	}
	v := uint32(*id) //nolint:gosec // talkgroup IDs are 24 bits
	return &v
}

func (q *requester) repeater(db *gorm.DB, id uint32) *pluginv1.Answer {
	var found []models.Repeater
	err := db.Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Where("id = ?", id).Limit(1).Find(&found).Error
	if err != nil {
		return q.unavailable("read a repeater", err)
	}
	if len(found) == 0 {
		return fail(pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, "no such repeater")
	}
	r := &found[0]
	return &pluginv1.Answer{Result: &pluginv1.Answer_Repeater{Repeater: &pluginv1.RepeaterInfo{
		Repeater: id, Callsign: r.Callsign,
		Slots: []*pluginv1.SlotInfo{
			{Slot: 1, StaticTalkgroups: talkgroupIDs(r.TS1StaticTalkgroups), DynamicTalkgroup: dynamicOf(r.TS1DynamicTalkgroupID)},
			{Slot: 2, StaticTalkgroups: talkgroupIDs(r.TS2StaticTalkgroups), DynamicTalkgroup: dynamicOf(r.TS2DynamicTalkgroupID)},
		},
	}}}
}

func (q *requester) sendText(ctx context.Context, t *pluginv1.SendText, ranges []models.PluginRange) *pluginv1.Answer {
	if q.s.texts == nil {
		return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, "this hub does not send texts for plugins")
	}
	source, radio := uint(t.GetSource()), uint(t.GetRadio())
	switch {
	case !inRanges(ranges, source):
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the source is not one of the plugin's IDs")
	case radio == 0 || radio > models.MaxDMRID:
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the radio is not a DMR ID")
	case t.GetBody() == "" || len([]rune(t.GetBody())) > maxTextBody:
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the text is not 1 to 500 characters")
	}
	if _, owned := q.s.registry.Owner(radio); owned {
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the radio is an ID of a plugin")
	}
	var format string
	var err error
	if t.ReplyTo != nil {
		if q.s.prompts == nil {
			return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, "this hub does not answer a text in its own format")
		}
		format, err = q.s.texts.ReplyText(ctx, source, t.GetReplyTo(), radio, t.GetBody())
	} else {
		format, err = q.s.texts.SendText(ctx, source, radio, t.GetBody())
	}
	switch {
	case errors.Is(err, ErrNoFormat):
		return fail(pluginv1.ErrorCode_ERROR_CODE_NO_FORMAT, err.Error())
	case errors.Is(err, ErrTextDoesNotFit):
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, err.Error())
	case errors.Is(err, ErrRadioRefused):
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, err.Error())
	case errors.Is(err, ErrNoAnswer):
		return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, err.Error())
	case err != nil:
		return q.unavailable("send a text", err)
	}
	return &pluginv1.Answer{Result: &pluginv1.Answer_TextSent{TextSent: &pluginv1.TextSent{Format: format}}}
}

func (q *requester) playPrompt(ctx context.Context, p *pluginv1.PlayPrompt, ranges []models.PluginRange) *pluginv1.Answer {
	if q.s.prompts == nil || q.s.texts == nil {
		return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, "this hub does not play prompts for plugins")
	}
	source, radio := uint(p.GetSource()), uint(p.GetRadio())
	switch {
	case !inRanges(ranges, source):
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the source is not one of the plugin's IDs")
	case radio == 0 || radio > models.MaxDMRID:
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the radio is not a DMR ID")
	case p.GetSlot() != 1 && p.GetSlot() != 2:
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the timeslot is not 1 or 2")
	case len(p.GetClips()) == 0 || len(p.GetClips()) > maxPromptClips:
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "not 1 to 64 clips")
	}
	if _, owned := q.s.registry.Owner(radio); owned {
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the radio is an ID of a plugin")
	}
	err := q.s.prompts.PlayPrompt(ctx, source, radio, p.GetSlot() == 2, p.GetClips(), uint(p.GetStreamId()))
	switch {
	case errors.Is(err, ErrUnknownClip):
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, err.Error())
	case errors.Is(err, ErrRadioNotFound):
		return fail(pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, err.Error())
	case err != nil:
		return q.unavailable("play a prompt", err)
	}
	return &pluginv1.Answer{}
}

// optedIn returns the callsigns, in upper case, of the users who turned a
// switch of plugin on.
func optedIn(db *gorm.DB, plugin uint) (map[string]bool, error) {
	on, err := models.PluginSwitchesOn(db, plugin)
	if err != nil {
		return nil, err //nolint:wrapcheck // PluginSwitchesOn wraps its errors
	}
	out := make(map[string]bool, len(on))
	for _, u := range on {
		out[strings.ToUpper(u.Callsign)] = true
	}
	return out, nil
}

func baseCall(address string) string {
	base, _, _ := strings.Cut(strings.ToUpper(address), "-")
	return base
}

func (q *requester) sendAPRS(ctx context.Context, a *pluginv1.APRSSend, now time.Time) *pluginv1.Answer {
	if q.s.aprs == nil || (q.s.aprs.On != nil && !q.s.aprs.On()) {
		return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, "this hub has no APRS-IS login")
	}
	m := aprs.Message{Source: strings.ToUpper(a.GetSource()), Addressee: a.GetAddressee(), Text: a.GetText(), Number: a.GetNumber()}
	if err := m.Check(); err != nil {
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, err.Error())
	}
	if !aprs.Licensed(baseCall(m.Source)) {
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the source is not a licensed callsign")
	}
	on, err := optedIn(q.s.db.WithContext(ctx), q.p.ID)
	if err != nil {
		return q.unavailable("check the users of the plugin", err)
	}
	if !on[baseCall(m.Source)] {
		return fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "no user with this callsign turned a switch of the plugin on")
	}
	switch err := q.countAPRS(ctx, now); {
	case errors.Is(err, errAPRSRate):
		return fail(pluginv1.ErrorCode_ERROR_CODE_RATE, errAPRSRate.Error())
	case err != nil:
		return q.unavailable("count the APRS messages", err)
	}
	if err := q.s.aprs.Send(m); err != nil {
		return q.unavailable("send an APRS message", err)
	}
	return &pluginv1.Answer{}
}

// emailOf returns the switch key of plugin and the confirmed email address
// of the user id, as the hub sends a plugin's email to the user, or the
// answer that refuses it.
func (q *requester) emailOf(id uint, key string) (string, *models.PluginSwitchSpec, *pluginv1.Answer) {
	var users []models.User
	if err := q.s.db.Where("id = ?", id).Limit(1).Find(&users).Error; err != nil {
		return "", nil, q.unavailable("look up a user", err)
	}
	if len(users) == 0 || !users[0].Approved || users[0].Suspended {
		return "", nil, fail(pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND, "no such user")
	}
	user := &users[0]
	switches, err := models.UserPluginSwitches(q.s.db, q.p.ID, user)
	if err != nil {
		return "", nil, q.unavailable("read the switches of a user", err)
	}
	var spec *models.PluginSwitchSpec
	for i := range switches {
		if switches[i].Spec.Key == key && switches[i].On {
			spec = &switches[i].Spec
		}
	}
	if spec == nil {
		return "", nil, fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the user did not turn this switch of the plugin on")
	}
	if user.Email == "" || !user.EmailVerified {
		return "", nil, fail(pluginv1.ErrorCode_ERROR_CODE_REFUSED, "the user has no confirmed email address")
	}
	return user.Email, spec, nil
}

func (q *requester) sendEmail(ctx context.Context, e *pluginv1.SendEmail, now time.Time) *pluginv1.Answer {
	if q.s.email == nil || (q.s.email.On != nil && !q.s.email.On()) {
		return fail(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE, "this hub does not send email")
	}
	subject, body := e.GetSubject(), e.GetBody()
	switch {
	case subject == "" || len([]rune(subject)) > maxEmailSubject || strings.ContainsAny(subject, "\r\n"):
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the subject is not 1 to 120 characters on one line")
	case body == "" || len([]rune(body)) > maxEmailBody:
		return fail(pluginv1.ErrorCode_ERROR_CODE_INVALID, "the body is not 1 to 4000 characters")
	}
	to, spec, refused := q.emailOf(uint(e.GetUserId()), e.GetSwitch())
	if refused != nil {
		return refused
	}
	switch err := q.countEmail(ctx, now); {
	case errors.Is(err, errEmailRate):
		return fail(pluginv1.ErrorCode_ERROR_CODE_RATE, errEmailRate.Error())
	case err != nil:
		return q.unavailable("count the emails", err)
	}
	body = strings.TrimRight(body, "\n") + "\n\n" + fmt.Sprintf("You get this email because you turned on %q of the %s plugin on your Settings page.\n", spec.Title, q.p.Name)
	if err := q.s.email.Send(to, subject, body); err != nil {
		return q.unavailable("send an email", err)
	}
	return &pluginv1.Answer{}
}
