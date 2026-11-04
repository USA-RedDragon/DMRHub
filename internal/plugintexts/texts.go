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

// Package plugintexts sends the text messages of plugins, in the format of a
// text that the radio itself sent through a repeater of its owner or one
// that an admin trusts, as store and forward reads it.
package plugintexts

import (
	"context"
	"errors"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/storeforward"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"gorm.io/gorm"
)

// Sender sends a data message to a repeater, as the hub does.
type Sender interface {
	// SendData sends m to the repeater on the slot (true for timeslot 2),
	// and returns how it ended.
	SendData(ctx context.Context, repeater uint, slot bool, m data.Message) (data.Result, error)
}

// Texts sends the texts of plugins through sender. It is a plugins.Texts.
type Texts struct {
	db     *gorm.DB
	sender Sender
}

// New returns Texts that read the formats of radios in db and send with
// sender.
func New(db *gorm.DB, sender Sender) *Texts {
	return &Texts{db: db, sender: sender}
}

// SendText sends body from source to radio through the repeater and
// timeslot of the text that the format comes from, and returns the name
// of the format. It sends nothing when the hub knows no format of the
// radio.
func (t *Texts) SendText(ctx context.Context, source, radio uint, body string) (string, error) {
	tpl, f, err := storeforward.RadioText(t.db.WithContext(ctx), radio)
	return t.send(ctx, tpl, f, err, source, radio, body)
}

// ReplyText sends body from source to radio in the format of the text id
// that radio sent to source, through the repeater and timeslot of that
// text, and returns the name of the format. It returns plugins.ErrNoFormat,
// and sends nothing, when that text came through a repeater of neither the
// radio's owner nor a trusted one, or in a format that the hub cannot build
// again exactly, and the errors of SendText.
func (t *Texts) ReplyText(ctx context.Context, source uint, id uint64, radio uint, body string) (string, error) {
	tpl, f, err := storeforward.MessageText(t.db.WithContext(ctx), id, radio, source)
	return t.send(ctx, tpl, f, err, source, radio, body)
}

func (t *Texts) send(ctx context.Context, tpl *models.Message, f *storeforward.Format, err error, source, radio uint, body string) (string, error) {
	switch {
	case errors.Is(err, storeforward.ErrNoFormat), errors.Is(err, storeforward.ErrNotReproducible):
		return "", fmt.Errorf("%w: %w", plugins.ErrNoFormat, err)
	case err != nil:
		return "", fmt.Errorf("reading the text format of radio %d: %w", radio, err)
	}
	m, err := f.Build(body, source, radio)
	if err != nil {
		return "", fmt.Errorf("%w: %w", plugins.ErrTextDoesNotFit, err)
	}
	result, err := t.sender.SendData(ctx, tpl.RepeaterID, tpl.Slot == 2, m)
	if err != nil {
		return "", fmt.Errorf("sending a text to radio %d: %w", radio, err)
	}
	switch result {
	case data.ResultSent, data.ResultDelivered:
		return f.Name(), nil
	case data.ResultRefused:
		return "", plugins.ErrRadioRefused
	case data.ResultCancelled:
		return "", fmt.Errorf("sending a text to radio %d: %w", radio, context.Canceled)
	case data.ResultRetryLimit, data.ResultTimeLimit:
	}
	return "", plugins.ErrNoAnswer
}
