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

package auth_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	forgotPath    = "/api/v1/auth/forgot-password"
	resetPath     = "/api/v1/auth/reset-password"
	checkPath     = "/api/v1/auth/reset-password/check"
	resetSent     = `{"message":"If that account has an email address, a link is on its way. It works for 1 hour."}`
	resetBadLink  = `{"error":"This link does not work. It is older than 1 hour or was used already. Ask for a new one."}`
	resetDone     = `{"message":"Your password is set. Log in with it now."}`
	mailTimeout   = 10 * time.Second
	resetHost     = "https://hub.example.com"
	resetUserMail = "n0call@example.com"
)

type sentMail struct {
	to, subject, body string
}

type fakeMailer struct {
	sent chan sentMail
}

func (f *fakeMailer) SendText(to, subject, body string) error {
	f.sent <- sentMail{to, subject, body}
	return nil
}

func (f *fakeMailer) next(t *testing.T) sentMail {
	t.Helper()
	select {
	case m := <-f.sent:
		return m
	case <-time.After(mailTimeout):
		require.FailNow(t, "no password reset email was sent")
		return sentMail{}
	}
}

var linkToken = regexp.MustCompile(`https://hub\.example\.com/reset-password#token=([0-9a-f]{64})\n`)

func (f *fakeMailer) token(t *testing.T) string {
	t.Helper()
	m := f.next(t)
	match := linkToken.FindStringSubmatch(m.body)
	require.Len(t, match, 2, "the email holds the link: %q", m.body)
	return match[1]
}

func resetRouter(t *testing.T, smtpOn bool) (*gin.Engine, *gorm.DB, *fakeMailer) {
	t.Helper()
	mailer := &fakeMailer{sent: make(chan sentMail, 32)}
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
		c.SMTP.Enabled = smtpOn
		c.HTTP.CanonicalHost = resetHost + "/"
		c.NetworkName = "Test Hub"
	}, dmrhttp.WithMailer(mailer))
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	db := tdb.DB()
	testutils.SeedAccount(t, db, 1000001, "N0CALL", "jdoe")
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", 1000001).Update("email", resetUserMail).Error)
	testutils.SeedAccount(t, db, 1000002, "N1CALL", "nomail")
	return router, db, mailer
}

func forgot(t *testing.T, router *gin.Engine, user string) string {
	t.Helper()
	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, forgotPath, map[string]string{"user": user})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w.Body.String()
}

func resets(t *testing.T, db *gorm.DB, id uint) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.PasswordReset{}).Where("user_id = ?", id).Count(&n).Error)
	return n
}

func TestForgotPasswordAnswersTheSameForEveryAccount(t *testing.T) {
	t.Parallel()
	router, db, mailer := resetRouter(t, true)

	for _, name := range []string{"nobody", "nomail", "N1CALL"} {
		assert.JSONEq(t, resetSent, forgot(t, router, name), name)
	}
	assert.Zero(t, resets(t, db, 1000002), "an account without an email gets no link")
	assert.Empty(t, mailer.sent)

	assert.JSONEq(t, resetSent, forgot(t, router, "n0call"), "a callsign in any case finds the account")
	m := mailer.next(t)
	assert.Equal(t, resetUserMail, m.to)
	assert.Equal(t, "Choose a new Test Hub password", m.subject)
	assert.Regexp(t, `^Someone asked for a new password for N0CALL on Test Hub\.\n\n`+
		`To choose one, open this link\. It works once, for 1 hour:\n`+
		`https://hub\.example\.com/reset-password#token=[0-9a-f]{64}\n\n`+
		`If you did not ask, ignore this email\. Your password stays the same\.\n$`, m.body)

	assert.JSONEq(t, resetSent, forgot(t, router, "jdoe"), "the username finds the account")
	mailer.next(t)
	assert.Equal(t, int64(2), resets(t, db, 1000001))

	var stored []models.PasswordReset
	require.NoError(t, db.Find(&stored).Error)
	for _, r := range stored {
		assert.Len(t, r.TokenHash, 32, "only the hash of the token is kept")
	}
}

func TestPasswordResetLinkWorksOnceAndEndsEverySession(t *testing.T) {
	t.Parallel()
	router, _, mailer := resetRouter(t, true)
	first := testutils.Login(t, router, "jdoe")
	second := testutils.Login(t, router, "jdoe")
	forgot(t, router, "jdoe")
	token := mailer.token(t)

	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, checkPath, map[string]string{"token": token})
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"callsign":"N0CALL"}`, w.Body.String())

	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, resetPath, map[string]string{"token": token})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"Enter a new password."}`, w.Body.String())

	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, resetPath, map[string]string{"token": token, "password": "my-new-password"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, resetDone, w.Body.String())
	for _, jar := range []testutils.CookieJar{first, second} {
		assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me", nil).Code, "every session ends")
	}
	_, lw, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "jdoe", Password: "my-new-password"})
	assert.Equal(t, http.StatusOK, lw.Code)

	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, resetPath, map[string]string{"token": token, "password": "another-password"})
	assert.Equal(t, http.StatusBadRequest, w.Code, "a link works once")
	assert.JSONEq(t, resetBadLink, w.Body.String())
	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, checkPath, map[string]string{"token": token})
	assert.JSONEq(t, resetBadLink, w.Body.String())
	_, lw, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{Username: "jdoe", Password: "my-new-password"})
	assert.Equal(t, http.StatusOK, lw.Code)
}

func TestPasswordResetLinkExpiresAfterAnHour(t *testing.T) {
	t.Parallel()
	router, db, _ := resetRouter(t, true)
	old, err := models.CreatePasswordReset(db, 1000001, time.Now().Add(-61*time.Minute))
	require.NoError(t, err)
	fresh, err := models.CreatePasswordReset(db, 1000001, time.Now().Add(-59*time.Minute))
	require.NoError(t, err)

	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, checkPath, map[string]string{"token": old})
	assert.JSONEq(t, resetBadLink, w.Body.String())
	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, resetPath, map[string]string{"token": old, "password": "my-new-password"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, resetBadLink, w.Body.String())
	testutils.Login(t, router, "jdoe")

	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, checkPath, map[string]string{"token": fresh})
	assert.Equal(t, http.StatusOK, w.Code, "a link of 59 minutes still works")
}

func TestAnyNewPasswordEndsTheResetLinks(t *testing.T) {
	t.Parallel()
	router, db, _ := resetRouter(t, true)
	mine, err := models.CreatePasswordReset(db, 1000001, time.Now())
	require.NoError(t, err)
	jar := testutils.Login(t, router, "jdoe")
	w := testutils.Do(t, router, jar, http.MethodPatch, "/api/v1/users/1000001", map[string]string{"password": "my-new-password", "current_password": testutils.TestPassword})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, resetPath, map[string]string{"token": mine, "password": "another-password"})
	assert.JSONEq(t, resetBadLink, w.Body.String(), "a change of the user's own password")

	byAdmin, err := models.CreatePasswordReset(db, 1000001, time.Now())
	require.NoError(t, err)
	admin := testutils.Login(t, router, "Admin")
	w = testutils.Do(t, router, admin, http.MethodPatch, "/api/v1/users/1000001", map[string]string{"password": "Qm4tVx8rKp2Hw9Ns"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, resetPath, map[string]string{"token": byAdmin, "password": "another-password"})
	assert.JSONEq(t, resetBadLink, w.Body.String(), "a reset by an admin")
	assert.Zero(t, resets(t, db, 1000001))
}

func TestForgotPasswordIsLimitedPerAccountAndPerAddress(t *testing.T) {
	t.Parallel()
	router, db, mailer := resetRouter(t, true)
	for range 4 {
		assert.JSONEq(t, resetSent, forgot(t, router, "jdoe"))
	}
	for range 3 {
		mailer.next(t)
	}
	assert.Equal(t, int64(3), resets(t, db, 1000001), "3 links per account and hour")

	require.NoError(t, db.Exec("DELETE FROM ratelimits WHERE key LIKE ?", "password-reset:user:%").Error)
	for i := range 6 {
		assert.JSONEq(t, resetSent, forgot(t, router, fmt.Sprintf("nobody%d", i)))
	}
	assert.JSONEq(t, resetSent, forgot(t, router, "jdoe"), "over the limit of the address the answer stays the same")
	assert.Equal(t, int64(3), resets(t, db, 1000001), "10 requests per address and hour")
}

func TestForgotPasswordNeedsEmail(t *testing.T) {
	t.Parallel()
	router, _, _ := resetRouter(t, false)
	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, forgotPath, map[string]string{"user": "jdoe"})
	assert.Equal(t, http.StatusNotFound, w.Code)

	for _, on := range []bool{false, true} {
		router, _, _ := resetRouter(t, on)
		w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodGet, "/api/v1/network/features", nil)
		var features struct {
			PasswordReset *bool `json:"password_reset"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &features))
		require.NotNil(t, features.PasswordReset)
		assert.Equal(t, on, *features.PasswordReset)
	}
}
