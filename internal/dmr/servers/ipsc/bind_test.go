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

package ipsc_test

import (
	"context"
	"net"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/ipsc"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenBracketedBindAddress(t *testing.T) {
	t.Parallel()

	for bind, want := range map[string]string{"[::]": "::", "[::1]": "::1", "127.0.0.1": "127.0.0.1"} {
		t.Run(bind, func(t *testing.T) {
			t.Parallel()

			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			cfg.DMR.IPSC.Bind = bind
			cfg.DMR.IPSC.Port = 0

			s := ipsc.NewIPSCServer(&cfg, nil, nil)
			require.NoError(t, s.Listen())
			defer func() { _ = s.Stop(context.Background()) }()

			local, ok := s.Addr().(*net.UDPAddr)
			require.True(t, ok)
			assert.Equal(t, want, local.IP.String())
		})
	}
}
