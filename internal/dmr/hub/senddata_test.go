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

package hub_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoDataMessagesToOneTimeslotGoOneAfterTheOther(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		var wg sync.WaitGroup
		for _, body := range []string{"first", "second"} {
			var hd sms.Header
			payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Source: uint32(answerService), Destination: uint32(answerRadio), Body: []byte(body)}, &hd)
			require.NoError(t, err)
			m := data.Message{Header: hd.Data, Rate: enums.DataTypeRate34, Payload: payload}
			wg.Go(func() {
				res, err := h.SendData(context.Background(), answerRepeater, true, m)
				assert.NoError(t, err)
				assert.Equal(t, data.ResultSent, res)
			})
		}
		wg.Wait()
		synctest.Wait()
		_, delivered := answered(t, srv)
		require.Len(t, delivered, 2, "both arrive whole, not mixed on the slot")
		bodies := make([]string, 0, len(delivered))
		for _, m := range delivered {
			body, _, err := sms.Decode(nil, &m.Header, m.Payload)
			require.NoError(t, err)
			bodies = append(bodies, string(body))
		}
		assert.ElementsMatch(t, []string{"first", "second"}, bodies)
	})
}
