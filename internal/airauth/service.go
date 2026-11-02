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

package airauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"gorm.io/gorm"
)

var (
	// ErrWrongCode is the error of a code that is not the code of the
	// secret now, or that was used already.
	ErrWrongCode = errors.New("wrong code")
	// ErrNoSetup is the error of a Confirm without a Begin in the last
	// SetupTTL.
	ErrNoSetup = errors.New("no on-air code setup is open")
	// ErrLimited is the error of a try after too many tries.
	ErrLimited = errors.New("too many tries")
	// ErrOff is the error of a change that needs the on-air code on.
	ErrOff = errors.New("the on-air code is off")
	// ErrOn is the error of a setup while the on-air code is on.
	ErrOn = errors.New("the on-air code is on")
	// ErrNotYours is the error of a Lock of a radio ID of another user.
	ErrNotYours = errors.New("the radio ID is not one of the user")

	errNoChange = errors.New("no change")
)

const (
	// SetupTTL is how long a secret from Begin waits for Confirm.
	SetupTTL = 15 * time.Minute
	// DefaultUnlock is how long a right code unlocks a radio ID when no
	// length is set.
	DefaultUnlock = 24 * time.Hour
	// stepTTL keeps the last used step of a user while a code of it can
	// still match.
	stepTTL = 5 * time.Minute
	// storeWait bounds each update of the store.
	storeWait = 5 * time.Second
	// rateWindow is the window of the limits of tries.
	rateWindow = 5 * time.Minute
	// SourceTries is the most tries of codes that one radio ID sends in
	// rateWindow.
	SourceTries = 5
	// IngressTries is the most tries of codes that one repeater or hotspot
	// carries in rateWindow.
	IngressTries = 20
	// UserTries is the most codes that one user enters on the web in
	// rateWindow.
	UserTries = 5
	keyPrefix = "dmrhub:airauth:"
)

// Grant is the unlock of the radio ID Radio on the slot Slot (1 or 2) of
// the repeater or hotspot Repeater until Until.
type Grant struct {
	Radio    uint      `json:"radio"`
	Repeater uint      `json:"repeater"`
	Slot     uint8     `json:"slot"`
	Until    time.Time `json:"until"`
}

// Unlocked reports whether grants unlock the radio on the slot of
// repeater at now. slot2 is true for slot 2.
func Unlocked(grants []Grant, repeater uint, slot2 bool, now time.Time) bool {
	s := SlotNumber(slot2)
	for _, g := range grants {
		if g.Repeater == repeater && g.Slot == s && now.Before(g.Until) {
			return true
		}
	}
	return false
}

// SlotNumber returns 2 for slot 2 and 1 for slot 1.
func SlotNumber(slot2 bool) uint8 {
	if slot2 {
		return 2
	}
	return 1
}

// Result is how a try of a code on the air ended.
type Result int

const (
	// Refused is a try with a wrong or used code, or from a radio ID whose
	// owner has no on-air code.
	Refused Result = iota
	// Granted is a try that unlocked the radio ID.
	Granted
	// Limited is a try after too many tries.
	Limited
	// Unavailable is a try that could not finish, such as when the store
	// did not answer. It does not count against the limits of tries.
	Unavailable
)

func (r Result) String() string {
	switch r {
	case Granted:
		return "ok"
	case Limited:
		return "limited"
	case Refused:
		return "fail"
	case Unavailable:
		return "error"
	}
	return "fail"
}

// Try is one code that the radio ID Src, of the user Owner, sent on the
// slot of the repeater or hotspot Repeater.
type Try struct {
	Src      uint
	Owner    uint
	Code     uint32
	Repeater uint
	Slot2    bool
}

// Service keeps the on-air codes in db and the store.
type Service struct {
	db        *gorm.DB
	store     kv.KV
	clock     clock.Clock
	key       func() []byte
	unlockFor func() time.Duration
}

// New returns a Service. key returns the key that seals the secrets (see
// KeyOf), and unlockFor the time that a right code unlocks a radio ID
// for; a length of 0 or less is DefaultUnlock.
func New(db *gorm.DB, store kv.KV, c clock.Clock, key func() []byte, unlockFor func() time.Duration) *Service {
	if c == nil {
		c = clock.Real()
	}
	return &Service{db: db, store: store, clock: c, key: key, unlockFor: unlockFor}
}

// update is store.Update bounded by storeWait, so that a key that keeps
// changing cannot hold a try or a page.
func (s *Service) update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	ctx, cancel := context.WithTimeout(ctx, storeWait)
	defer cancel()
	return s.store.Update(ctx, key, ttl, fn) //nolint:wrapcheck // the callers wrap it
}

func (s *Service) unlockLength() time.Duration {
	if s.unlockFor == nil {
		return DefaultUnlock
	}
	if d := s.unlockFor(); d > 0 {
		return d
	}
	return DefaultUnlock
}

// Enabled reports whether the on-air code of the user userID is on.
func (s *Service) Enabled(userID uint) (bool, error) {
	if s.db == nil {
		return false, ErrOff
	}
	sealed, err := models.TOTPSecretOf(s.db, userID)
	if err != nil {
		return false, err //nolint:wrapcheck // TOTPSecretOf wraps its errors
	}
	return sealed != "", nil
}

func (s *Service) secretOf(userID uint) ([]byte, error) {
	sealed, err := models.TOTPSecretOf(s.db, userID)
	if err != nil {
		return nil, err //nolint:wrapcheck // TOTPSecretOf wraps its errors
	}
	if sealed == "" {
		return nil, ErrOff
	}
	return Open(s.key(), userID, sealed)
}

// Setup is a new secret for a user, which the user adds to an
// authenticator app.
type Setup struct {
	Key string
	URI string
}

func pendingKey(userID uint) string {
	return keyPrefix + "setup:" + strconv.FormatUint(uint64(userID), 10)
}
func stepKey(userID uint) string { return keyPrefix + "step:" + strconv.FormatUint(uint64(userID), 10) }
func grantsKey(radio uint) string {
	return keyPrefix + "grants:" + strconv.FormatUint(uint64(radio), 10)
}
func rateKey(kind string, id uint) string {
	return keyPrefix + "rate:" + kind + ":" + strconv.FormatUint(uint64(id), 10)
}

// Begin makes a new secret for the user userID and keeps it, sealed, for
// SetupTTL, until Confirm turns it on.
func (s *Service) Begin(ctx context.Context, userID uint, issuer, account string) (Setup, error) {
	on, err := s.Enabled(userID)
	if err != nil {
		return Setup{}, err
	}
	if on {
		return Setup{}, ErrOn
	}
	secret := NewSecret()
	sealed, err := Seal(s.key(), userID, secret)
	if err != nil {
		return Setup{}, err
	}
	err = s.update(ctx, pendingKey(userID), SetupTTL, func([]byte) ([]byte, error) { return []byte(sealed), nil })
	if err != nil {
		return Setup{}, fmt.Errorf("keep the on-air code setup: %w", err)
	}
	return Setup{Key: EncodeSecret(secret), URI: URI(issuer, account, secret)}, nil
}

// Confirm turns the on-air code of the user userID on when code is a code
// of the secret of the last Begin.
func (s *Service) Confirm(ctx context.Context, userID uint, code uint32) error {
	if ok, err := s.allow(ctx, rateKey("user", userID), UserTries); err != nil || !ok {
		return limitedOr(err)
	}
	b, err := s.store.Get(ctx, pendingKey(userID))
	if errors.Is(err, kv.ErrNotFound) {
		return ErrNoSetup
	}
	if err != nil {
		return fmt.Errorf("read the on-air code setup: %w", err)
	}
	secret, err := Open(s.key(), userID, string(b))
	if err != nil {
		return err
	}
	if err := s.check(ctx, userID, secret, code); err != nil {
		return err
	}
	if err := models.SetTOTPSecret(s.db, userID, string(b)); err != nil {
		return err //nolint:wrapcheck // SetTOTPSecret wraps its errors
	}
	if err := s.store.Delete(ctx, pendingKey(userID)); err != nil {
		slog.Warn("Could not delete an on-air code setup", "userID", userID, "error", err)
	}
	return nil
}

// Disable turns the on-air code of the user userID off when code is a
// code of the secret now, and ends the unlocks of the radio IDs of the
// user. It returns the radio IDs whose unlocks ended.
func (s *Service) Disable(ctx context.Context, userID uint, code uint32) ([]uint, error) {
	if ok, err := s.allow(ctx, rateKey("user", userID), UserTries); err != nil || !ok {
		return nil, limitedOr(err)
	}
	secret, err := s.secretOf(userID)
	if err != nil {
		return nil, err
	}
	if err := s.check(ctx, userID, secret, code); err != nil {
		return nil, err
	}
	return s.Reset(ctx, userID)
}

// Reset turns the on-air code of the user userID off without a code, as
// an admin does for a user who lost their app, and ends the unlocks of
// the radio IDs of the user. It returns the radio IDs whose unlocks ended.
func (s *Service) Reset(ctx context.Context, userID uint) ([]uint, error) {
	if err := models.SetTOTPSecret(s.db, userID, ""); err != nil {
		return nil, err //nolint:wrapcheck // SetTOTPSecret wraps its errors
	}
	radios, err := s.radios(userID)
	if err != nil {
		return nil, err
	}
	for _, r := range radios {
		if err := s.store.Delete(ctx, grantsKey(r)); err != nil {
			return nil, fmt.Errorf("end the unlocks of a radio: %w", err)
		}
	}
	return radios, nil
}

func (s *Service) radios(userID uint) ([]uint, error) {
	ids, err := models.ListUserRadioIDs(s.db, userID)
	if err != nil {
		return nil, err //nolint:wrapcheck // ListUserRadioIDs wraps its errors
	}
	out := []uint{userID}
	for _, id := range ids {
		out = append(out, id.RadioID)
	}
	return out, nil
}

func limitedOr(err error) error {
	if err != nil {
		return err
	}
	return ErrLimited
}

// check reports ErrWrongCode unless code is a code of secret now, in a
// time step after the last one that the user userID used.
func (s *Service) check(ctx context.Context, userID uint, secret []byte, code uint32) error {
	_, _, err := s.use(ctx, userID, secret, code)
	return err
}

// use marks the time step of code used for the user userID, as check does,
// and returns that step and the step that was marked before it, 0 for
// none, for unuse.
func (s *Service) use(ctx context.Context, userID uint, secret []byte, code uint32) (int64, int64, error) {
	step, ok := Match(secret, code, s.clock.Now())
	if !ok {
		return 0, 0, ErrWrongCode
	}
	var prev int64
	err := s.update(ctx, stepKey(userID), stepTTL, func(old []byte) ([]byte, error) {
		prev = 0
		if old != nil {
			last, err := strconv.ParseInt(string(old), 10, 64)
			if err == nil && step <= last {
				return nil, errNoChange
			}
			if err == nil {
				prev = last
			}
		}
		return []byte(strconv.FormatInt(step, 10)), nil
	})
	if errors.Is(err, errNoChange) {
		return 0, 0, ErrWrongCode
	}
	if err != nil {
		return 0, 0, fmt.Errorf("mark the code used: %w", err)
	}
	return step, prev, nil
}

// unuse marks the time step prev used again for the user userID, in place
// of step, when step is still the last one marked, so that a code whose
// unlock could not be kept works again.
func (s *Service) unuse(ctx context.Context, userID uint, step, prev int64) error {
	err := s.update(ctx, stepKey(userID), stepTTL, func(old []byte) ([]byte, error) {
		if string(old) != strconv.FormatInt(step, 10) {
			return nil, errNoChange
		}
		return []byte(strconv.FormatInt(prev, 10)), nil
	})
	if err != nil && !errors.Is(err, errNoChange) {
		return fmt.Errorf("give back a used code: %w", err)
	}
	return nil
}

type window struct {
	Start int64 `json:"s"`
	N     int   `json:"n"`
}

// allow counts a try against key and reports whether it is within limit
// tries in rateWindow.
func (s *Service) allow(ctx context.Context, key string, limit int) (bool, error) {
	now := s.clock.Now().UnixNano()
	err := s.update(ctx, key, rateWindow, func(old []byte) ([]byte, error) {
		var w window
		if old != nil {
			_ = json.Unmarshal(old, &w)
		}
		if old == nil || now-w.Start >= int64(rateWindow) || now < w.Start {
			w = window{Start: now}
		}
		if w.N >= limit {
			return nil, errNoChange
		}
		w.N++
		return json.Marshal(w) //nolint:wrapcheck // a window always marshals
	})
	if errors.Is(err, errNoChange) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("count a try of a code: %w", err)
	}
	return true, nil
}

// Attempt checks the code of t and, when it is right, unlocks the radio
// ID of t on its repeater slot for the unlock length. It logs the source
// and the result, never the code, and logs why a try could not finish.
func (s *Service) Attempt(ctx context.Context, t Try) Result {
	r, err := s.attempt(ctx, t)
	if err != nil {
		slog.Warn("On-air code try could not finish", "src", t.Src, "error", err)
	}
	slog.Info("On-air code try", "src", t.Src, "result", r.String())
	return r
}

func (s *Service) attempt(ctx context.Context, t Try) (Result, error) {
	var counted []string
	unavailable := func(err error) (Result, error) {
		for _, key := range counted {
			if rerr := s.uncount(ctx, key); rerr != nil {
				err = errors.Join(err, rerr)
			}
		}
		return Unavailable, err
	}
	for _, l := range []struct {
		key   string
		limit int
	}{{rateKey("src", t.Src), SourceTries}, {rateKey("in", t.Repeater), IngressTries}} {
		ok, err := s.allow(ctx, l.key, l.limit)
		if err != nil {
			return unavailable(err)
		}
		if !ok {
			return Limited, nil
		}
		counted = append(counted, l.key)
	}
	if t.Owner == 0 || s.db == nil {
		return Refused, nil
	}
	secret, err := s.secretOf(t.Owner)
	if errors.Is(err, ErrOff) || errors.Is(err, ErrSealed) {
		return Refused, nil
	}
	if err != nil {
		return unavailable(err)
	}
	step, prev, err := s.use(ctx, t.Owner, secret, t.Code)
	if errors.Is(err, ErrWrongCode) {
		return Refused, nil
	}
	if err != nil {
		return unavailable(err)
	}
	if err := s.grant(ctx, t.Src, t.Repeater, t.Slot2); err != nil {
		if uerr := s.unuse(ctx, t.Owner, step, prev); uerr != nil {
			err = errors.Join(err, uerr)
		}
		return unavailable(err)
	}
	return Granted, nil
}

// uncount takes back one try that allow counted against key in the same
// window.
func (s *Service) uncount(ctx context.Context, key string) error {
	err := s.update(ctx, key, rateWindow, func(old []byte) ([]byte, error) {
		var w window
		if old == nil || json.Unmarshal(old, &w) != nil || w.N == 0 {
			return nil, errNoChange
		}
		w.N--
		return json.Marshal(w) //nolint:wrapcheck // a window always marshals
	})
	if err != nil && !errors.Is(err, errNoChange) {
		return fmt.Errorf("take back a try of a code: %w", err)
	}
	return nil
}

func (s *Service) grant(ctx context.Context, radio, repeater uint, slot2 bool) error {
	now := s.clock.Now()
	g := Grant{Radio: radio, Repeater: repeater, Slot: SlotNumber(slot2), Until: now.Add(s.unlockLength())}
	return s.updateGrants(ctx, radio, func(list []Grant) []Grant {
		list = slices.DeleteFunc(list, func(o Grant) bool { return o.Repeater == g.Repeater && o.Slot == g.Slot })
		return append(list, g)
	})
}

func (s *Service) updateGrants(ctx context.Context, radio uint, change func([]Grant) []Grant) error {
	now := s.clock.Now()
	err := s.update(ctx, grantsKey(radio), s.unlockLength()+Period, func(old []byte) ([]byte, error) {
		var list []Grant
		if old != nil {
			_ = json.Unmarshal(old, &list)
		}
		list = change(slices.DeleteFunc(list, func(g Grant) bool { return !now.Before(g.Until) }))
		if list == nil {
			list = []Grant{}
		}
		return json.Marshal(list) //nolint:wrapcheck // a list of grants always marshals
	})
	if err != nil {
		return fmt.Errorf("keep the unlocks of a radio: %w", err)
	}
	return nil
}

// Grants returns the unlocks of the radio ID radio that have not ended.
func (s *Service) Grants(ctx context.Context, radio uint) ([]Grant, error) {
	b, err := s.store.Get(ctx, grantsKey(radio))
	if errors.Is(err, kv.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the unlocks of a radio: %w", err)
	}
	var list []Grant
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("read the unlocks of a radio: %w", err)
	}
	now := s.clock.Now()
	return slices.DeleteFunc(list, func(g Grant) bool { return !now.Before(g.Until) }), nil
}

// UserGrants returns the unlocks of the radio IDs of the user userID that
// have not ended.
func (s *Service) UserGrants(ctx context.Context, userID uint) ([]Grant, error) {
	radios, err := s.radios(userID)
	if err != nil {
		return nil, err
	}
	var out []Grant
	for _, r := range radios {
		list, err := s.Grants(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

// Lock ends the unlock of the radio ID radio of the user userID on the
// slot of repeater.
func (s *Service) Lock(ctx context.Context, userID, radio, repeater uint, slot uint8) error {
	radios, err := s.radios(userID)
	if err != nil {
		return err
	}
	if !slices.Contains(radios, radio) {
		return ErrNotYours
	}
	return s.updateGrants(ctx, radio, func(list []Grant) []Grant {
		return slices.DeleteFunc(list, func(g Grant) bool { return g.Repeater == repeater && g.Slot == slot })
	})
}
