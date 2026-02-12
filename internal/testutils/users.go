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
package testutils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const testTimeout = 1 * time.Minute

type CookieJar struct {
	cookies []http.Cookie
}

func (t *CookieJar) SetCookies(cookies []http.Cookie) {
	t.cookies = cookies
}

func (t *CookieJar) Cookies() []http.Cookie {
	return t.cookies
}

func RegisterUser(t *testing.T, router *gin.Engine, user apimodels.UserRegistration) (APIResponse, *httptest.ResponseRecorder) {
	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/users", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func LoginUser(t *testing.T, router *gin.Engine, user apimodels.AuthLogin) (APIResponse, *httptest.ResponseRecorder, CookieJar) {
	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/auth/login", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)

	jar := CookieJar{}
	assert.NoError(t, err)

	ckies := w.Result().Cookies()

	cookies := make([]http.Cookie, 0, len(ckies))
	for _, cookie := range ckies {
		cookies = append(cookies, *cookie)
	}

	jar.SetCookies(cookies)

	return resp, w, jar
}

type APIResponseUserList struct {
	Total int           `json:"total"`
	Users []models.User `json:"users"`
}

func ListUsers(t *testing.T, router *gin.Engine, jar CookieJar) (APIResponseUserList, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users", nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponseUserList
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func DeleteUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func GetUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (models.User, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp models.User
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func GetUserMe(t *testing.T, router *gin.Engine, jar CookieJar) (models.User, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/users/me", nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp models.User
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func SuspendUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/suspend/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func UnsuspendUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/unsuspend/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func LoginAdmin(t *testing.T, router *gin.Engine) (APIResponse, *httptest.ResponseRecorder, CookieJar) {
	admin := apimodels.AuthLogin{
		Username: "Admin",
		Password: "password",
	}

	return LoginUser(t, router, admin)
}

func CreateAndLoginUser(t *testing.T, router *gin.Engine, user apimodels.UserRegistration) (APIResponse, *httptest.ResponseRecorder, CookieJar) {
	resp, w := RegisterUser(t, router, user)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Body.String())

	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	admin := apimodels.AuthLogin{
		Username: "Admin",
		Password: "password",
	}

	resp, w, jar := LoginUser(t, router, admin)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Body.String())

	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = ApproveUser(t, router, user.DMRId, jar)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Body.String())
	assert.Empty(t, resp.Error)
	assert.Equal(t, "User approved", resp.Message)

	return LoginUser(t, router, apimodels.AuthLogin{
		Username: user.Username,
		Password: user.Password,
	})
}

func ApproveUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/approve/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func PromoteUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/promote/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func DemoteUser(t *testing.T, router *gin.Engine, dmrID uint, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/demote/%d", dmrID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	return resp, w
}

func LogoutUser(t *testing.T, router *gin.Engine, jar CookieJar) (APIResponse, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/logout", nil)
	assert.NoError(t, err)
	router.ServeHTTP(w, req)

	var resp APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	return resp, w
}
