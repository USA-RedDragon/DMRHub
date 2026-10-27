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

package utils_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
)

func configParserGet(raw string) (string, bool) {
	if raw != strings.TrimSpace(raw) {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			b.WriteByte(raw[i])
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		return "", false
	}
	return b.String(), true
}

func mmdvmConfValue(value string) string {
	if len(value) > 1 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	if i := strings.IndexByte(value, '#'); i >= 0 {
		value = value[:i]
	}
	return strings.TrimRight(value, " \t")
}

func escapeShellCmd(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if strings.IndexByte("#&;`|*?~<>^()[]{}$\\\n\xff'\"", c) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

var shellSafeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func checkExternalConfigs(pw string) []string {
	var bad []string
	if got, ok := configParserGet(pw); !ok || got != pw {
		bad = append(bad, "configparser PASSPHRASE (FreeDMR, HBlink3)")
	}
	if mmdvmConfValue(pw) != pw {
		bad = append(bad, "unquoted Password= (MMDVMHost, DMRGateway, MMDVM_Bridge, YSF2DMR)")
	}
	if mmdvmConfValue(`"`+pw+`"`) != pw {
		bad = append(bad, "quoted Password= (MMDVMHost, DMRGateway)")
	}
	if strings.ContainsAny(pw, "?{}|&~!()^\";$'") {
		bad = append(bad, "PHP parse_ini_file of the MMDVMHost configuration (Pi-Star, WPSD)")
	}
	if f := strings.Fields("DMRHub\t0000\tdmrhub.example.org\t" + pw + "\t62031"); len(f) != 5 || f[3] != pw {
		bad = append(bad, "DMR_Hosts.txt columns (Pi-Star, WPSD)")
	}
	if f := strings.Split(escapeShellCmd("dmrhub.example.org,"+pw+",62031,DMRHub"), ","); len(f) != 4 || f[1] != pw {
		bad = append(bad, "escapeshellcmd and explode of the DMR master (Pi-Star)")
	}
	if f := strings.Fields("DMRHub 1 dmrhub.example.org " + pw + " 62031"); len(f) != 5 || f[3] != pw || strings.Contains(pw, ",") {
		bad = append(bad, "DMRHosts.txt (DroidStar)")
	}
	if !shellSafeWord.MatchString(pw) {
		bad = append(bad, "unquoted shell word")
	}
	if strings.ContainsAny(pw[:1], "?:,[]{}#&*!|>'\"%@`") || strings.Contains(pw, ": ") || strings.Contains(pw, " #") {
		bad = append(bad, "plain YAML scalar (ipsc2mmdvm)")
	}
	if strings.ContainsAny(pw, "\"\\") {
		bad = append(bad, "JSON string (HBlink4)")
	}
	return bad
}

func TestGeneratedPasswordsFitExternalConfigs(t *testing.T) {
	t.Parallel()
	gens := map[string]func() (string, error){
		"peer":     utils.NewPeerPassword,
		"repeater": utils.NewRepeaterPassword,
		"ipsc":     func() (string, error) { return utils.RandomHexString(40) },
	}
	const n = 2000
	for name, gen := range gens {
		failed := 0
		for i := 0; i < n; i++ {
			pw, err := gen()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if bad := checkExternalConfigs(pw); len(bad) > 0 {
				failed++
				if failed <= 3 {
					t.Errorf("%s password %q breaks: %s", name, pw, strings.Join(bad, "; "))
				}
			}
		}
		if failed > 0 {
			t.Errorf("%s: %d of %d passwords break an external configuration", name, failed, n)
		}
	}
}

func TestCheckExternalConfigsRejectsUnsafe(t *testing.T) {
	t.Parallel()
	for _, pw := range []string{"ab%cd!x@2Kq", "abc#def", "abc;def", "abc,def", "abc def", "abc$def", "abc&def", "abc^def", "abc*def", "abc!def", "@abcdef"} {
		if len(checkExternalConfigs(pw)) == 0 {
			t.Errorf("%q passed the checks", pw)
		}
	}
	for _, pw := range []string{"kG7tZ2pQ9wX4", "Ab3dE6-gH_jK2", "0123456789abcdef0123456789abcdef01234567"} {
		if bad := checkExternalConfigs(pw); len(bad) > 0 {
			t.Errorf("%q failed: %s", pw, strings.Join(bad, "; "))
		}
	}
}
