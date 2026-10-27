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
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	verifyPath  = "/api/v1/auth/verify-email"
	myEmailPath = "/api/v1/users/me/email"
)

var verifyToken = regexp.MustCompile(`https://hub\.example\.com/verify-email#token=([0-9a-f]{64})\n`)

func emailRouter(t *testing.T, smtpOn bool) (*gin.Engine, *gorm.DB, *fakeMailer) {
	t.Helper()
	mailer := &fakeMailer{sent: make(chan sentMail, 32)}
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) {
		c.SMTP.Enabled = smtpOn
		c.HTTP.CanonicalHost = resetHost + "/"
		c.NetworkName = "Test Hub"
		c.DMR.DisableRadioIDValidation = true
	}, dmrhttp.WithMailer(mailer))
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	testutils.SeedAccount(t, tdb.DB(), 1000001, "N0CALL", "jdoe")
	return router, tdb.DB(), mailer
}

func verificationToken(t *testing.T, mailer *fakeMailer, to string) string {
	t.Helper()
	m := mailer.next(t)
	assert.Equal(t, to, m.to)
	assert.Equal(t, "Confirm your Test Hub email address", m.subject)
	match := verifyToken.FindStringSubmatch(m.body)
	require.Len(t, match, 2, "the email holds the link: %q", m.body)
	return match[1]
}

func storedEmail(t *testing.T, db *gorm.DB, id uint) (string, bool) {
	t.Helper()
	var u struct {
		Email         string
		EmailVerified bool
	}
	require.NoError(t, db.Table("users").Select("email", "email_verified").Where("id = ?", id).Scan(&u).Error)
	return u.Email, u.EmailVerified
}

func verify(t *testing.T, router *gin.Engine, token string) int {
	t.Helper()
	return testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, verifyPath, map[string]string{"token": token}).Code
}

func TestSignupEmailIsVerifiedByItsLinkOnce(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)

	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, "/api/v1/users", map[string]any{
		"id": 3100001, "callsign": "N0NEW", "username": "newbie", "password": "a long unique password", "email": "n0new@example.com",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	email, verified := storedEmail(t, db, 3100001)
	assert.Equal(t, "n0new@example.com", email)
	assert.False(t, verified, "a new email is not verified")

	token := verificationToken(t, mailer, "n0new@example.com")
	assert.Equal(t, http.StatusOK, verify(t, router, token))
	_, verified = storedEmail(t, db, 3100001)
	assert.True(t, verified, "the link verifies the email")
	assert.Equal(t, http.StatusBadRequest, verify(t, router, token), "a link works once")
}

func TestSignupWithoutEmailSendsNothing(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)
	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, "/api/v1/users", map[string]any{
		"id": 3100001, "callsign": "N0NEW", "username": "newbie", "password": "a long unique password",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	email, _ := storedEmail(t, db, 3100001)
	assert.Empty(t, email)
	assert.Empty(t, mailer.sent)
}

func TestEmailFormatIsChecked(t *testing.T) {
	t.Parallel()
	router, _, mailer := emailRouter(t, true)
	jar := testutils.Login(t, router, "jdoe")
	for _, bad := range []string{"not an email", "a@", "N0CALL <n0call@example.com>", "a@b.com\r\nBcc: x@y.com", "@example.com"} {
		w := testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": bad})
		assert.Equal(t, http.StatusBadRequest, w.Code, bad)
		assert.JSONEq(t, `{"error":"Enter an email address, such as n0call@example.com"}`, w.Body.String(), bad)
	}
	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, "/api/v1/users", map[string]any{
		"id": 3100001, "callsign": "N0NEW", "username": "newbie", "password": "a long unique password", "email": "nope",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, mailer.sent)
}

func TestChangedEmailIsUnverifiedUntilItsLink(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)
	jar := testutils.Login(t, router, "jdoe")

	w := testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "first@example.com"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	first := verificationToken(t, mailer, "first@example.com")
	require.Equal(t, http.StatusOK, verify(t, router, first))

	w = testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "second@example.com"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	email, verified := storedEmail(t, db, 1000001)
	assert.Equal(t, "second@example.com", email)
	assert.False(t, verified, "a changed email is not verified")
	second := verificationToken(t, mailer, "second@example.com")

	w = testutils.Do(t, router, jar, http.MethodGet, "/api/v1/users/me", nil)
	assert.Contains(t, w.Body.String(), `"email":"second@example.com"`)
	assert.Contains(t, w.Body.String(), `"email_verified":false`)

	require.Equal(t, http.StatusOK, verify(t, router, second))
	_, verified = storedEmail(t, db, 1000001)
	assert.True(t, verified)

	w = testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": ""})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	email, verified = storedEmail(t, db, 1000001)
	assert.Empty(t, email)
	assert.False(t, verified)
	assert.Empty(t, mailer.sent, "clearing the email sends nothing")
}

func TestOldLinkDoesNotVerifyANewerEmail(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)
	jar := testutils.Login(t, router, "jdoe")
	require.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "first@example.com"}).Code)
	first := verificationToken(t, mailer, "first@example.com")
	require.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "second@example.com"}).Code)
	verificationToken(t, mailer, "second@example.com")

	assert.Equal(t, http.StatusBadRequest, verify(t, router, first))
	_, verified := storedEmail(t, db, 1000001)
	assert.False(t, verified)
}

func TestExpiredLinkDoesNotVerify(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)
	jar := testutils.Login(t, router, "jdoe")
	require.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "first@example.com"}).Code)
	token := verificationToken(t, mailer, "first@example.com")
	require.NoError(t, db.Model(&models.EmailVerification{}).Where("user_id = ?", 1000001).Update("expires_at", time.Now().Add(-time.Minute)).Error)
	assert.Equal(t, http.StatusBadRequest, verify(t, router, token))
	_, verified := storedEmail(t, db, 1000001)
	assert.False(t, verified)
}

func TestEmailNeedsSMTP(t *testing.T) {
	t.Parallel()
	router, _, _ := emailRouter(t, false)
	jar := testutils.Login(t, router, "jdoe")
	w := testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "first@example.com"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"This network cannot send email"}`, w.Body.String())
}

func TestPasswordResetGoesOnlyToAVerifiedEmail(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)
	jar := testutils.Login(t, router, "jdoe")
	require.Equal(t, http.StatusOK, testutils.Do(t, router, jar, http.MethodPut, myEmailPath, map[string]string{"email": "first@example.com"}).Code)
	token := verificationToken(t, mailer, "first@example.com")

	forgot(t, router, "jdoe")
	assert.Zero(t, resets(t, db, 1000001), "an unverified email gets no reset link")
	assert.Empty(t, mailer.sent)

	require.Equal(t, http.StatusOK, verify(t, router, token))
	forgot(t, router, "jdoe")
	m := mailer.next(t)
	assert.Equal(t, "first@example.com", m.to)
	assert.Equal(t, "Choose a new Test Hub password", m.subject)
}

func TestAdminSetsAnUnverifiedEmail(t *testing.T) {
	t.Parallel()
	router, db, mailer := emailRouter(t, true)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	w := testutils.Do(t, router, adminJar, http.MethodPatch, "/api/v1/users/1000001", map[string]string{"email": "byadmin@example.com"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	email, verified := storedEmail(t, db, 1000001)
	assert.Equal(t, "byadmin@example.com", email)
	assert.False(t, verified)
	verificationToken(t, mailer, "byadmin@example.com")

	w = testutils.Do(t, router, adminJar, http.MethodGet, "/api/v1/users/1000001", nil)
	assert.Contains(t, w.Body.String(), `"email":"byadmin@example.com"`)
}

func TestUserListsHideEmails(t *testing.T) {
	t.Parallel()
	router, db, _ := emailRouter(t, true)
	require.NoError(t, db.Exec("UPDATE users SET email = ? WHERE id = ?", "secret@example.com", 1000001).Error)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	w := testutils.Do(t, router, adminJar, http.MethodGet, "/api/v1/users?limit=none", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "secret@example.com")
	w = testutils.Do(t, router, adminJar, http.MethodGet, "/api/v1/users/1000001/profile", nil)
	assert.NotContains(t, w.Body.String(), "secret@example.com")
}
