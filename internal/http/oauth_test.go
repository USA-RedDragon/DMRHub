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

package http_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/positions"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appOrigin   = "https://app.example"
	appCallback = "https://app.example/callback"
	appVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	hubOrigin   = "http://example.com"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

type oauthEnv struct {
	router *gin.Engine
	tdb    *testutils.TestDB
	jar    testutils.CookieJar
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	return &oauthEnv{router: router, tdb: tdb, jar: testutils.Login(t, router, "Admin")}
}

func appChallenge() string {
	sum := sha256.Sum256([]byte(appVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeQuery(scope string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {appOrigin},
		"redirect_uri":          {appCallback},
		"scope":                 {scope},
		"state":                 {"xyz"},
		"code_challenge":        {appChallenge()},
		"code_challenge_method": {"S256"},
	}
}

func (e *oauthEnv) request(t *testing.T, method, path string, header http.Header, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := strings.NewReader(body)
	req, err := http.NewRequestWithContext(context.Background(), method, path, r)
	require.NoError(t, err)
	req.Host = "example.com"
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *oauthEnv) consent(t *testing.T, scope string, approve bool, origin string) *httptest.ResponseRecorder {
	t.Helper()
	q := authorizeQuery(scope)
	body := map[string]any{"approve": approve}
	for k := range q {
		body[k] = q.Get(k)
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)
	h := http.Header{"Content-Type": {"application/json"}}
	if origin != "" {
		h.Set("Origin", origin)
	}
	for _, c := range e.jar.Cookies() {
		h.Add("Cookie", c.String())
	}
	return e.request(t, http.MethodPost, "/api/v1/oauth/authorize", h, string(b))
}

func (e *oauthEnv) form(t *testing.T, path string, v url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return e.request(t, http.MethodPost, path, http.Header{
		"Content-Type": {"application/x-www-form-urlencoded"},
		"Origin":       {appOrigin},
	}, v.Encode())
}

func (e *oauthEnv) token(t *testing.T, scope string) tokenResponse {
	t.Helper()
	w := e.consent(t, scope, true, hubOrigin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Redirect string `json:"redirect"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	u, err := url.Parse(out.Redirect)
	require.NoError(t, err)
	assert.Equal(t, appCallback, u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, "xyz", u.Query().Get("state"))
	assert.NotEmpty(t, u.Query().Get("iss"))
	w = e.form(t, "/api/v1/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {u.Query().Get("code")},
		"redirect_uri":  {appCallback},
		"client_id":     {appOrigin},
		"code_verifier": {appVerifier},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	var tok tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tok))
	assert.Equal(t, "Bearer", tok.TokenType)
	assert.Equal(t, 900, tok.ExpiresIn)
	return tok
}

func (e *oauthEnv) bearer(t *testing.T, method, path, access string) *httptest.ResponseRecorder {
	t.Helper()
	return e.request(t, method, path, http.Header{"Authorization": {"Bearer " + access}, "Origin": {appOrigin}}, "")
}

func TestOAuthAuthorizeShowsTheAppAndWhatItAsksFor(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	w := e.request(t, http.MethodGet, "/api/v1/oauth/authorize?"+authorizeQuery("messages:read talkgroups:read").Encode(), nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Origin string `json:"origin"`
		Scopes []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"scopes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, appOrigin, out.Origin)
	require.Len(t, out.Scopes, 2)
	assert.Equal(t, "See the talkgroups of this network", out.Scopes[0].Description)

	q := authorizeQuery("talkgroups:read")
	q.Set("redirect_uri", "https://evil.example/cb")
	w = e.request(t, http.MethodGet, "/api/v1/oauth/authorize?"+q.Encode(), nil, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "evil.example/cb?")

	q = authorizeQuery("admin")
	w = e.request(t, http.MethodGet, "/api/v1/oauth/authorize?"+q.Encode(), nil, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "error=invalid_scope")
}

func TestOAuthTokenActsAsTheUserWithinItsScopes(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	other := testutils.SeedAccount(t, e.tdb.DB(), 1000099, "N0CALL", "other")
	tok := e.token(t, "talkgroups:read lastheard:read")

	w := e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"username":"Admin"`)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Set-Cookie"))

	assert.Equal(t, http.StatusOK, e.bearer(t, http.MethodGet, "/api/v1/talkgroups/my", tok.AccessToken).Code)

	w = e.bearer(t, http.MethodGet, "/api/v1/messages/inbox", tok.AccessToken)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`)

	assert.Equal(t, http.StatusForbidden, e.bearer(t, http.MethodGet, "/api/v1/repeaters", tok.AccessToken).Code, "admin routes are never for apps")
	assert.Equal(t, http.StatusForbidden, e.bearer(t, http.MethodPost, "/api/v1/auth/logout", tok.AccessToken).Code)

	w = e.bearer(t, http.MethodGet, fmt.Sprintf("/api/v1/lastheard/user/%d", other.ID), tok.AccessToken)
	assert.Equal(t, http.StatusForbidden, w.Code, "a token of an admin has no admin rights")

	w = e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken+"x")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
}

func TestOAuthRefreshRevokeAndApps(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	tok := e.token(t, "talkgroups:read")

	w := e.form(t, "/api/v1/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {appOrigin}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var next tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &next))
	assert.NotEqual(t, tok.RefreshToken, next.RefreshToken)

	w = e.form(t, "/api/v1/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"nope"}, "client_id": {appOrigin}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"error":"invalid_grant"`)

	w = testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/oauth/apps", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var apps []struct {
		ID     string `json:"id"`
		Origin string `json:"origin"`
		CanUse string `json:"can_use"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apps))
	require.Len(t, apps, 1)
	assert.Equal(t, appOrigin, apps[0].Origin)
	assert.Equal(t, "Talkgroups", apps[0].CanUse)
	assert.Equal(t, http.StatusForbidden, e.bearer(t, http.MethodGet, "/api/v1/oauth/apps", next.AccessToken).Code, "an app cannot list or remove apps")

	w = e.form(t, "/api/v1/oauth/revoke", url.Values{"token": {next.RefreshToken}, "client_id": {appOrigin}})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, http.StatusUnauthorized, e.bearer(t, http.MethodGet, "/api/v1/users/me", next.AccessToken).Code)

	again := e.token(t, "talkgroups:read")
	w = testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/oauth/apps", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apps))
	require.Len(t, apps, 1)
	w = testutils.Do(t, e.router, e.jar, http.MethodDelete, "/api/v1/oauth/apps/"+apps[0].ID, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, http.StatusUnauthorized, e.bearer(t, http.MethodGet, "/api/v1/users/me", again.AccessToken).Code)
}

func TestOAuthConsentComesOnlyFromTheHubsOwnPage(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	assert.Equal(t, http.StatusForbidden, e.consent(t, "talkgroups:read", true, "https://evil.example").Code)

	h := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Origin": {hubOrigin}}
	for _, c := range e.jar.Cookies() {
		h.Add("Cookie", c.String())
	}
	q := authorizeQuery("talkgroups:read")
	q.Set("approve", "true")
	assert.Equal(t, http.StatusUnsupportedMediaType, e.request(t, http.MethodPost, "/api/v1/oauth/authorize", h, q.Encode()).Code)

	e.jar = testutils.CookieJar{}
	assert.Equal(t, http.StatusUnauthorized, e.consent(t, "talkgroups:read", true, hubOrigin).Code)
}

func TestOAuthDenyTellsTheApp(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	w := e.consent(t, "talkgroups:read", false, hubOrigin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "https://app.example/callback?error=access_denied")
	assert.Contains(t, w.Body.String(), "state=xyz")
}

func TestOAuthAppsMayCallFromAnyOrigin(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	for _, path := range []string{"/api/v1/oauth/token", "/api/v1/oauth/revoke", "/api/v1/users/me", "/.well-known/oauth-authorization-server"} {
		w := e.request(t, http.MethodOptions, path, http.Header{
			"Origin":                         {appOrigin},
			"Access-Control-Request-Method":  {http.MethodPost},
			"Access-Control-Request-Headers": {"authorization, content-type"},
		}, "")
		assert.Equal(t, http.StatusNoContent, w.Code, path)
		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"), path)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"), path)
		assert.Contains(t, strings.ToLower(w.Header().Get("Access-Control-Allow-Headers")), "authorization", path)
	}
	w := e.request(t, http.MethodOptions, "/api/v1/oauth/authorize", http.Header{
		"Origin":                        {appOrigin},
		"Access-Control-Request-Method": {http.MethodPost},
	}, "")
	assert.NotEqual(t, "*", w.Header().Get("Access-Control-Allow-Origin"), "consent is never for other origins")
}

func TestOAuthDiscovery(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	w := e.request(t, http.MethodGet, "/.well-known/oauth-authorization-server", http.Header{"Origin": {appOrigin}}, "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	issuer, _ := doc["issuer"].(string)
	require.NotEmpty(t, issuer)
	assert.Equal(t, issuer+"/oauth/authorize", doc["authorization_endpoint"])
	assert.Equal(t, issuer+"/api/v1/oauth/token", doc["token_endpoint"])
	assert.Equal(t, issuer+"/api/v1/oauth/revoke", doc["revocation_endpoint"])
	assert.Equal(t, []any{"S256"}, doc["code_challenge_methods_supported"])
	assert.Equal(t, []any{"code"}, doc["response_types_supported"])
	assert.Equal(t, []any{"none"}, doc["token_endpoint_auth_methods_supported"])
	assert.Contains(t, doc["scopes_supported"], "hotspots:talkgroups")
}

func TestOAuthTokenOpensTheMessagesWebsocketFromAnotherSite(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()

	tok := e.token(t, "messages:read")
	conn, resp := testutils.DialWSProtocols(t, server.URL, "/ws/messages", appOrigin, "dmrhub", "dmrhub.bearer."+tok.AccessToken)
	require.NotNil(t, conn, "status %d", resp.StatusCode)
	assert.Equal(t, "dmrhub", resp.Header.Get("Sec-WebSocket-Protocol"))
	conn.WriteText(t, "PING")
	text, err := conn.ReadText(3 * time.Second)
	require.NoError(t, err)
	assert.Equal(t, "PONG", text)

	narrow := e.token(t, "talkgroups:read")
	conn, resp = testutils.DialWSProtocols(t, server.URL, "/ws/messages", appOrigin, "dmrhub", "dmrhub.bearer."+narrow.AccessToken)
	assert.Nil(t, conn)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	conn, resp = testutils.DialWSProtocols(t, server.URL, "/ws/messages", appOrigin, "dmrhub")
	assert.Nil(t, conn)
	assert.NotEqual(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestOAuthTokenGetsOnlyTheFramesOfItsScopesOnTheMessagesWebsocket(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()
	var admin models.User
	require.NoError(t, e.tdb.DB().Where("username = ?", "Admin").Take(&admin).Error)

	for scope, want := range map[string]string{
		"positions:read": `{"kind":"position"}`,
		"messages:read":  `{"boxes":["inbox"]}`,
	} {
		tok := e.token(t, scope)
		conn, resp := testutils.DialWSProtocols(t, server.URL, "/ws/messages", appOrigin, "dmrhub", "dmrhub.bearer."+tok.AccessToken)
		require.NotNil(t, conn, "status %d", resp.StatusCode)
		conn.WriteText(t, "PING")
		text, err := conn.ReadText(3 * time.Second)
		require.NoError(t, err)
		require.Equal(t, "PONG", text)

		require.NoError(t, e.tdb.PubSub().Publish(positions.Topic(admin.ID), []byte(`{"kind":"position"}`)))
		require.NoError(t, e.tdb.PubSub().Publish(messages.Topic(admin.ID), []byte(`{"boxes":["inbox"]}`)))
		var got []string
		for {
			text, err := conn.ReadText(500 * time.Millisecond)
			if err != nil {
				break
			}
			got = append(got, text)
		}
		assert.Equal(t, []string{want}, got, scope)
	}
}

func TestOAuthRemovingAnAppClosesItsWebsockets(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	server := httptest.NewServer(e.router)
	defer server.Close()
	var admin models.User
	require.NoError(t, e.tdb.DB().Where("username = ?", "Admin").Take(&admin).Error)

	user, resp := testutils.DialWS(t, server.URL, "/ws/messages", e.jar)
	require.NotNil(t, user, "status %d", resp.StatusCode)
	defer user.Close()

	removals := []struct {
		name   string
		remove func(tok tokenResponse)
	}{
		{"revoke", func(tok tokenResponse) {
			w := e.form(t, "/api/v1/oauth/revoke", url.Values{"token": {tok.RefreshToken}, "client_id": {appOrigin}})
			require.Equal(t, http.StatusOK, w.Code)
		}},
		{"remove", func(_ tokenResponse) {
			w := testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/oauth/apps", nil)
			var apps []struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apps))
			require.Len(t, apps, 1)
			w = testutils.Do(t, e.router, e.jar, http.MethodDelete, "/api/v1/oauth/apps/"+apps[0].ID, nil)
			require.Equal(t, http.StatusOK, w.Code)
		}},
	}
	for _, r := range removals {
		tok := e.token(t, "messages:read")
		conn, resp := testutils.DialWSProtocols(t, server.URL, "/ws/messages", appOrigin, "dmrhub", "dmrhub.bearer."+tok.AccessToken)
		require.NotNil(t, conn, "status %d", resp.StatusCode)
		conn.WriteText(t, "PING")
		text, err := conn.ReadText(3 * time.Second)
		require.NoError(t, err)
		require.Equal(t, "PONG", text)

		r.remove(tok)
		text, err = conn.ReadText(3 * time.Second)
		if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
			require.NoError(t, e.tdb.PubSub().Publish(messages.Topic(admin.ID), []byte(`{"boxes":["inbox"]}`)))
			got, _ := conn.ReadText(2 * time.Second)
			require.Failf(t, "socket still open", "%s: 3 s after the app was removed its socket is open (first frame %q) and still gets %q", r.name, text, got)
		}
		var closed *testutils.WSCloseError
		require.ErrorAs(t, err, &closed, r.name)
		assert.Equal(t, 1008, closed.Code, r.name)
		assert.Equal(t, "app removed", closed.Reason, r.name)

		user.WriteText(t, "PING")
		text, err = user.ReadText(3 * time.Second)
		require.NoError(t, err, "%s: the user's own socket was closed", r.name)
		require.Equal(t, "PONG", text, r.name)
	}
}

func seedHotspot(t *testing.T, e *oauthEnv, id uint, owner string) {
	t.Helper()
	var user models.User
	require.NoError(t, e.tdb.DB().Where("username = ?", owner).Take(&user).Error)
	require.NoError(t, e.tdb.DB().Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id, Callsign: user.Callsign}, OwnerID: user.ID, Type: models.RepeaterTypeMMDVM}).Error)
}

func TestOAuthUsersMeShowsHotspotsOnlyToAHotspotScope(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	seedHotspot(t, e, 311860, "Admin")
	repeaters := func(w *httptest.ResponseRecorder) (json.RawMessage, bool) {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		r, ok := out["repeaters"]
		return r, ok
	}

	tok := e.token(t, "talkgroups:read")
	_, ok := repeaters(e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken))
	assert.False(t, ok, "a token without a hotspot scope does not see the hotspots")

	for _, scope := range []string{"hotspots:read", "hotspots:talkgroups"} {
		tok = e.token(t, scope)
		r, ok := repeaters(e.bearer(t, http.MethodGet, "/api/v1/users/me", tok.AccessToken))
		require.True(t, ok, scope)
		assert.Contains(t, string(r), `"id":311860`, scope)
	}

	r, ok := repeaters(testutils.Do(t, e.router, e.jar, http.MethodGet, "/api/v1/users/me", nil))
	require.True(t, ok, "the web session keeps the hotspots")
	assert.Contains(t, string(r), `"id":311860`)
}

func TestChangingTheTalkgroupsOfAnotherUsersHotspotIsForbidden(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	testutils.SeedAccount(t, e.tdb.DB(), 1000099, "N0CALL", "other")
	seedHotspot(t, e, 311860, "Admin")
	seedHotspot(t, e, 100009901, "other")
	body := `{"ts1_static_talkgroups":[],"ts2_static_talkgroups":[]}`
	forbidden := func(w *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.JSONEq(t, `{"error":"You do not own this repeater"}`, w.Body.String())
	}

	tok := e.token(t, "hotspots:talkgroups")
	h := http.Header{"Authorization": {"Bearer " + tok.AccessToken}, "Origin": {appOrigin}, "Content-Type": {"application/json"}}
	forbidden(e.request(t, http.MethodPost, "/api/v1/repeaters/100009901/talkgroups", h, body))

	jar := testutils.Login(t, e.router, "other")
	forbidden(testutils.Do(t, e.router, jar, http.MethodPost, "/api/v1/repeaters/311860/talkgroups", map[string]any{"ts1_static_talkgroups": []any{}, "ts2_static_talkgroups": []any{}}))
	w := testutils.Do(t, e.router, testutils.CookieJar{}, http.MethodPost, "/api/v1/repeaters/311860/talkgroups", map[string]any{})
	assert.Equal(t, http.StatusUnauthorized, w.Code, "no session is still 401")
}

func TestOAuthTokenSeesOnlyTheHotspotsOfItsUser(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	testutils.SeedAccount(t, e.tdb.DB(), 1000099, "N0CALL", "other")
	seedHotspot(t, e, 311860, "Admin")
	seedHotspot(t, e, 100009901, "other")

	for _, scope := range []string{"hotspots:read", "hotspots:talkgroups"} {
		tok := e.token(t, scope)
		w := e.bearer(t, http.MethodGet, "/api/v1/repeaters/311860", tok.AccessToken)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"id":311860`)
		w = e.bearer(t, http.MethodGet, "/api/v1/repeaters/100009901", tok.AccessToken)
		require.Equal(t, http.StatusForbidden, w.Code, "%s: %s", scope, w.Body.String())
		assert.JSONEq(t, `{"error":"You do not own this repeater"}`, w.Body.String())
	}

	jar := testutils.Login(t, e.router, "other")
	w := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/repeaters/311860", nil)
	require.Equal(t, http.StatusOK, w.Code, "a web session still sees the repeaters of other users")
	assert.Contains(t, w.Body.String(), `"id":311860`)
}
