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

package lastheard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	alice     = uint(3191868)
	bob       = uint(3140598)
	aliceRptr = uint(319186801)
	tg        = uint(3100)
)

type page struct {
	Calls []apimodels.WSCallResponse `json:"calls"`
	Total int                        `json:"total"`
}

func seedUser(t *testing.T, database *gorm.DB, id uint, name string) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	hash, err := utils.HashPassword("password", cfg.PasswordSalt)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.User{ID: id, Callsign: strings.ToUpper(name), Username: name, Password: hash, Approved: true, Email: name + "@example.com"}).Error)
}

func login(t *testing.T, router *gin.Engine, name string) testutils.CookieJar {
	t.Helper()
	_, w, jar := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: name, Password: "password"})
	require.Equal(t, http.StatusOK, w.Code)
	return jar
}

func get(t *testing.T, router *gin.Engine, path string, jar *testutils.CookieJar) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	require.NoError(t, err)
	if jar != nil {
		for _, cookie := range jar.Cookies() {
			req.Header.Add("Cookie", cookie.String())
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func getPage(t *testing.T, router *gin.Engine, path string, jar *testutils.CookieJar) page {
	t.Helper()
	code, body := get(t, router, path, jar)
	require.Equal(t, http.StatusOK, code, body)
	var p page
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	return p
}

func kinds(p page) []string {
	out := make([]string, 0, len(p.Calls))
	for _, c := range p.Calls {
		k := string(c.Kind)
		if c.SubKind == models.SubKindSMS {
			k = "sms"
		}
		out = append(out, k)
	}
	return out
}

func setup(t *testing.T) *gin.Engine {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	database := tdb.DB()
	seedUser(t, database, alice, "alice")
	seedUser(t, database, bob, "bob")
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: aliceRptr, Callsign: "ALICE"},
		OwnerID:               alice,
		Type:                  models.RepeaterTypeMMDVM,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: "USA"}).Error)

	base := time.Now().Add(-time.Hour)
	toTG := tg
	sap := uint8(4)
	group := func(i int, kind models.CallKind, sub string, src uint) {
		c := models.Call{
			StreamID: uint(i), StartTime: base.Add(time.Duration(i) * time.Minute), Kind: kind, SubKind: sub,
			SourceID: src, UserID: &src, GroupCall: true, DestinationID: tg, IsToTalkgroup: true, ToTalkgroupID: &toTG,
		}
		if sub == models.SubKindSMS {
			c.SAP, c.Octets, c.DataFormat = &sap, 44, "ip"
		}
		require.NoError(t, database.Create(&c).Error)
	}
	group(1, models.CallKindVoice, "", alice)
	group(2, models.CallKindData, models.SubKindSMS, bob)
	group(3, models.CallKindUnknown, "reserved", bob)
	group(4, models.CallKindIdle, "", bob)
	group(5, models.CallKindCSBK, "preamble", bob)

	rptr := aliceRptr
	registered := func(id uint) *uint {
		if id != alice && id != bob {
			return nil
		}
		return &id
	}
	priv := func(i int, src, dst uint) {
		require.NoError(t, database.Create(&models.Call{
			StreamID: uint(i), StartTime: base.Add(time.Duration(i) * time.Minute), Kind: models.CallKindVoice,
			SourceID: src, UserID: registered(src), RepeaterID: &rptr, IngressID: rptr, DestinationID: dst, IsToUser: true, ToUserID: registered(dst),
		}).Error)
	}
	priv(10, bob, alice)
	priv(11, 3113042, 3999999)
	return router
}

func TestLastheardShowsKindsAndHidesUnknownAndIdle(t *testing.T) {
	t.Parallel()
	router := setup(t)

	code, body := get(t, router, "/api/v1/lastheard", nil)
	require.Equal(t, http.StatusOK, code)
	for _, key := range []string{`"email"`, `"admin"`, `"superAdmin"`, `"suspended"`, `"password"`} {
		assert.NotContains(t, body, key)
	}
	var p page
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	assert.ElementsMatch(t, []string{"voice", "data", "csbk"}, kinds(p))
	assert.Equal(t, 3, p.Total)
	for _, c := range p.Calls {
		if c.Kind == models.CallKindData {
			assert.Empty(t, c.SubKind, "a visitor sees an SMS as plain data")
			assert.Zero(t, c.Octets)
			assert.Nil(t, c.SAP)
		}
	}

	p = getPage(t, router, "/api/v1/lastheard?types=unknown", nil)
	assert.Equal(t, []string{"unknown"}, kinds(p))
	p = getPage(t, router, "/api/v1/lastheard?types=control&idle=true", nil)
	assert.Equal(t, []string{"idle"}, kinds(p))
	p = getPage(t, router, "/api/v1/lastheard?types=sms", nil)
	assert.Empty(t, p.Calls, "a visitor sees no SMS")

	code, _ = get(t, router, "/api/v1/lastheard?types=secret", nil)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestLastheardShowsSMSToAdmins(t *testing.T) {
	t.Parallel()
	router := setup(t)
	_, _, admin := testutils.LoginAdmin(t, router)

	p := getPage(t, router, "/api/v1/lastheard?types=sms", &admin)
	require.Len(t, p.Calls, 1)
	assert.Equal(t, models.SubKindSMS, p.Calls[0].SubKind)
	assert.Equal(t, uint(44), p.Calls[0].Octets)
	assert.Equal(t, "ip", p.Calls[0].DataFormat)

	aliceJar := login(t, router, "alice")
	p = getPage(t, router, "/api/v1/lastheard?types=sms", &aliceJar)
	assert.Empty(t, p.Calls, "a user sees no SMS outside their window")
}

func TestLastheardPrivateHistoryIsClosed(t *testing.T) {
	t.Parallel()
	router := setup(t)
	aliceJar := login(t, router, "alice")
	bobJar := login(t, router, "bob")
	_, _, admin := testutils.LoginAdmin(t, router)

	p := getPage(t, router, "/api/v1/lastheard/user/3191868", &aliceJar)
	assert.Equal(t, 2, p.Total, "alice sees her group call and the private call to her")

	code, _ := get(t, router, "/api/v1/lastheard/user/3191868", &bobJar)
	assert.Equal(t, http.StatusUnauthorized, code, "bob cannot read alice's history")
	code, _ = get(t, router, "/api/v1/lastheard/user/3191868", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	p = getPage(t, router, "/api/v1/lastheard/user/3191868", &admin)
	assert.Equal(t, 2, p.Total)

	p = getPage(t, router, "/api/v1/lastheard/repeater/319186801", &aliceJar)
	assert.Equal(t, 2, p.Total, "the owner sees the calls through her repeater")
	code, _ = get(t, router, "/api/v1/lastheard/repeater/319186801", &bobJar)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestLastheardTalkgroupTotalIsNotPaged(t *testing.T) {
	t.Parallel()
	router := setup(t)

	p := getPage(t, router, "/api/v1/lastheard/talkgroup/3100?limit=1&page=1", nil)
	assert.Len(t, p.Calls, 1)
	assert.Equal(t, 3, p.Total)
}
