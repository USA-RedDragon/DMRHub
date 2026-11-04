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
	"errors"

	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
)

// IsText returns the text of m, a data packet to one of the plugin's IDs,
// and true when m is a text that the hub reads: a text in a format of
// sms.Decode, or a UDT in a text format, whose Text is empty. The hub
// answers each such packet itself, with the response of the data link
// layer that a confirmed text asks for, a NACK for any packet that came
// damaged, and the TMS acknowledgement that a TMS text asks for, so that a
// radio gets one answer. A plugin that answers its own confirmed data, as
// with a data.Receiver that responds, sends only the ACKs of the packets
// that are not texts.
func IsText(m *data.Message) (sms.Text, bool) {
	if u, ok := m.Header.AsUDT(); ok {
		f := u.UDTFormat
		return sms.Text{}, f == enums.UDTISO7 || f == enums.UDTISO8 || f == enums.UDTUTF16BE
	}
	_, t, err := sms.Decode(nil, &m.Header, m.Payload)
	return t, err == nil || errors.Is(err, ip.ErrChecksum)
}
