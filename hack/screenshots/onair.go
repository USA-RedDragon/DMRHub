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

package main

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"rsc.io/qr"
)

const exampleOnAirKey = "JBSWY3DPEHPK3PXPMFRGGZDF"

// alignToStep moves the clock to 5 s into the next time step of the
// on-air codes, so that a code made now still holds after the steps of a
// request or a call.
func (r *runner) alignToStep() error {
	into := time.Duration(r.cursor.UnixNano() % int64(airauth.Period))
	return r.advance(airauth.Period - into + 5*time.Second)
}

func (r *runner) onAirCode(secret []byte, at time.Time) uint32 {
	return airauth.Code(secret, airauth.Step(at))
}

// turnOnAirCodeOn turns on the on-air code of N3CALL and unlocks radio
// 3000004 on the slot 2 of its hotspot and on the slot 1 of the Dallas
// repeater, with calls to 9 and the code.
func turnOnAirCodeOn(ctx context.Context, r *runner) error {
	a := r.api("n3call")
	var setup struct {
		Key string `json:"key"`
	}
	if err := a.do(ctx, http.MethodPost, "/api/v1/users/me/totp/setup", nil, &setup); err != nil {
		return err
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	if err != nil {
		return fmt.Errorf("read the on-air key: %w", err)
	}
	if err := r.alignToStep(); err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", r.onAirCode(secret, r.cursor))
	if err := a.do(ctx, http.MethodPost, "/api/v1/users/me/totp", map[string]string{"code": code}, nil); err != nil {
		return err
	}
	for _, c := range []call{
		{repeater: 300000401, src: 3000004, slot2: true, frames: 6},
		{repeater: 990001, src: 3000004, frames: 6},
	} {
		if err := r.advance(airauth.Period); err != nil {
			return err
		}
		c.dst = dmrconst.UnlockFirst + uint(r.onAirCode(secret, r.cursor.Add(callSlot)))
		if err := r.play(c); err != nil {
			return err
		}
		time.Sleep(time.Second)
	}
	return nil
}

// exampleOnAirSetup shows an example key and its QR code in the open
// setup of the on-air code, in place of the random ones, so that the shot
// is the same on each run.
func exampleOnAirSetup(ctx context.Context, r *runner) error {
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(exampleOnAirKey)
	if err != nil {
		return fmt.Errorf("read the example on-air key: %w", err)
	}
	img, err := exampleQR(airauth.URI("DMRHub Demo", "N3CALL", secret))
	if err != nil {
		return err
	}
	key, err := json.Marshal("JBSW Y3DP EHPK 3PXP MFRG GZDF")
	if err != nil {
		return fmt.Errorf("show the example on-air key: %w", err)
	}
	src, err := json.Marshal(img)
	if err != nil {
		return fmt.Errorf("show the example QR code: %w", err)
	}
	return r.b.eval(ctx, `(() => {
		document.querySelector('[data-test="on-air-key"]').textContent = `+string(key)+`;
		const q = document.querySelector('[data-test="on-air-qr"]');
		q.src = `+string(src)+`;
		return q.decode();
	})()`, nil)
}

func exampleQR(uri string) (string, error) {
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return "", fmt.Errorf("draw the example QR code: %w", err)
	}
	code.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
