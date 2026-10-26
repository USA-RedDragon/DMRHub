# OAuth Apps

A web app can use a DMRHub account with OAuth 2.0. The user signs in to DMRHub, sees what the app asks for, and clicks **Allow**. The app then gets tokens that let it read and change a small set of things as that user. This page is for the people who write such apps.

DMRHub uses the authorization code flow with PKCE (RFC 7636), for public clients. An app has no client secret.

Throughout this document, `<siteurl>` is a placeholder for the URL of your DMRHub instance.

## Register an app

An app needs no registration. No admin or user adds it first, and there is no setting to turn OAuth on.

- The client ID of an app is its origin: the scheme, the host and the port, as in `https://app.example` or `http://localhost:8080`. Write it in lower case, with no path and no trailing slash.
- The origin must use HTTPS. Plain HTTP works only when the host is `localhost` or a loopback address, such as `127.0.0.1` or `[::1]`, so that you can test on your own machine.
- The redirect URI must be on that same origin. It can have any path and a query, but no fragment and no user name or password.

The user allows the app on the consent page of DMRHub. Each user sees the apps they allowed in **Settings**, in the **Apps** card, with what each app can use, the day they allowed it, and when it was last used. **Remove** signs the app out at once.

## Endpoints

DMRHub publishes its endpoints at `<siteurl>/.well-known/oauth-authorization-server` (RFC 8414).

| Endpoint                     | Path                                           |
| ---------------------------- | ---------------------------------------------- |
| Discovery                    | `GET /.well-known/oauth-authorization-server`  |
| Authorization (consent page) | `GET /oauth/authorize`                         |
| Token                        | `POST /api/v1/oauth/token`                     |
| Revocation                   | `POST /api/v1/oauth/revoke`                    |

The `issuer` is the canonical host of the DMRHub instance (`http.canonical-host` in the configuration). When it is not set, DMRHub uses the scheme and host of the request.

The token and revocation endpoints take `application/x-www-form-urlencoded` bodies. They answer any origin with `Access-Control-Allow-Origin: *` and without credentials, so a browser app can call them directly. Do not send cookies to them.

## Scopes

Ask for the scopes as a space-separated list in `scope`. Each name must be on this list, and you must ask for at least one. The consent page shows the text in the second column.

| Scope                 | What the user sees on the consent page                                    | Routes the token may use                                                                                                 |
| --------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `talkgroups:read`     | See the talkgroups of this network                                        | `GET /api/v1/talkgroups`, `GET /api/v1/talkgroups/my`, `GET /api/v1/talkgroups/:id`                                      |
| `lastheard:read`      | See last heard for your radio IDs and your hotspots                       | `GET /api/v1/lastheard`, `GET /api/v1/lastheard/user/:id`, `GET /api/v1/lastheard/repeater/:id`, `GET /api/v1/lastheard/talkgroup/:id` |
| `messages:read`       | Read your text messages, and get new ones as they arrive                  | `GET /api/v1/messages/inbox`, `GET /api/v1/messages/outbox`, `GET /api/v1/messages/stats`, `GET /api/v1/messages/:id`, `GET /ws/messages` |
| `positions:read`      | See where your radios are, and the radios shared with you, as they report | `GET /api/v1/positions`, `GET /api/v1/positions/:id`, `GET /api/v1/positions/:id/track`, `GET /ws/messages`              |
| `hotspots:read`       | See your hotspots                                                         | `GET /api/v1/repeaters/my`, `GET /api/v1/repeaters/:id`                                                                  |
| `hotspots:register`   | Add hotspots to your account and see their passwords                      | `POST /api/v1/repeaters`                                                                                                 |
| `hotspots:talkgroups` | Change the static talkgroups of your hotspots                             | `POST /api/v1/repeaters/:id/talkgroups`, `GET /api/v1/repeaters/my`, `GET /api/v1/repeaters/:id`                         |

Every token, whatever its scopes, may also use these routes:

- `GET /api/v1/users/me`: the account of the user, with `sms_storage` and `position_tracking`.
- `GET /api/v1/userdb/:id`: the RadioID.net entry of a DMR ID.
- `GET /api/v1/network/webtransport`: how to reach the WebTransport listener.

All other routes refuse a token with `403` and `insufficient_scope`, also when the user is signed in to DMRHub in the same browser.

A token acts as the user who allowed the app, and answers the same as the web pages do for that user:

- A token never has admin rights, also when the user is an admin. The routes that need an owner, such as the last heard of a hotspot or the talkgroups of a hotspot, check that the user owns it.
- `/api/v1/lastheard` gives the calls of the network, and adds the details that the user may see of their own radio IDs and hotspots.
- The messages routes and the `messages` frames of `/ws/messages` give the messages only when the user turned on SMS Storage. See [Messages and SMS Storage](messages.md).
- [Positions](positions.md#for-apps) tells what the positions routes give.
- `POST /api/v1/repeaters` with `{"id": 123456701}` adds a hotspot, and answers its password once. `POST /api/v1/repeaters/:id/talkgroups` with `{"ts1_static_talkgroups": [{"id": 3100}], "ts2_static_talkgroups": []}` replaces both lists of static talkgroups.
- Routes that return a list take `page` and `limit`. `limit` is 10 by default and 100 at most.

### Ask for every scope at once

DMRHub keeps one grant for each app and user. When the user allows the app again, the new scopes replace the old ones, and the tokens that the app already has get the new scopes too. They do not add up. Each time you send the user to the consent page, ask for every scope that the app needs.

## The flow

1. The app makes a code verifier: a random string of 43 to 128 characters from `A-Z`, `a-z`, `0-9`, `-`, `.`, `_` and `~`, as RFC 7636 asks. DMRHub refuses a verifier of another length. The code challenge is the SHA-256 of the verifier, in base64url without padding. DMRHub accepts only `S256`.
2. The app makes a random `state`, and keeps it with the verifier.
3. The app sends the browser to `<siteurl>/oauth/authorize` with `response_type=code`, `client_id`, `redirect_uri`, `scope`, `state`, `code_challenge` and `code_challenge_method=S256`.
4. When the user is not signed in, DMRHub sends them to the login page first, and then back. The consent page shows the origin of the app, the account, and what the app asks for.
5. When the user clicks **Allow**, DMRHub sends the browser to the redirect URI with `code`, `state` and `iss`. When the user clicks **Don't allow**, it sends `error=access_denied`, `state` and `iss`.
6. The app checks that `state` is the one it made and that `iss` is the issuer of DMRHub. Then it sends the code and the verifier to the token endpoint, within 2 minutes.
7. The app calls the API with `Authorization: Bearer <access token>`, and refreshes the access token before it expires.
8. When the user signs out of the app, the app revokes its refresh token.

### Tokens and lifetimes

| Item                    | Lifetime                                                                       |
| ----------------------- | ------------------------------------------------------------------------------ |
| Authorization code      | 2 minutes, and one exchange                                                    |
| Access token            | 15 minutes (`expires_in` is 900)                                               |
| Refresh token           | 30 days from when it was issued, and one use                                   |
| Grant                   | Until the user removes the app or the app revokes it. The app keeps access while it refreshes at least once each 30 days. |

Access tokens start with `dmrhub_at.` and refresh tokens with `dmrhub_rt.`. Treat both as opaque strings. DMRHub keeps only a hash of each refresh token and code.

Each refresh gives a new access token and a new refresh token, and spends the old refresh token. The old refresh token still works for 10 seconds after its first use, so that two tabs of the app that refresh at once both keep access. A use of a spent refresh token after those 10 seconds ends the grant: DMRHub takes it as a sign that someone other than the app has the token. Every token of the grant then stops working, and the user must allow the app again.

DMRHub also ends every grant of a user when their password is reset with a reset link, and when an admin sets a password for them. A change of the user's own password in Settings does not end the grants. Removing a user removes their grants.

## Worked example

This example runs against a DMRHub on loopback at `http://127.0.0.1:3905`, with the canonical host set to that URL. The app is at `http://localhost:8080`, and its redirect URI is `http://localhost:8080/callback`. The responses are real, with the secrets replaced by placeholders in angle brackets.

### Look up the endpoints

```bash
curl -s http://127.0.0.1:3905/.well-known/oauth-authorization-server
```

```json
{"authorization_endpoint":"http://127.0.0.1:3905/oauth/authorize","authorization_response_iss_parameter_supported":true,"code_challenge_methods_supported":["S256"],"grant_types_supported":["authorization_code","refresh_token"],"issuer":"http://127.0.0.1:3905","response_types_supported":["code"],"revocation_endpoint":"http://127.0.0.1:3905/api/v1/oauth/revoke","revocation_endpoint_auth_methods_supported":["none"],"scopes_supported":["talkgroups:read","lastheard:read","messages:read","positions:read","hotspots:read","hotspots:register","hotspots:talkgroups"],"token_endpoint":"http://127.0.0.1:3905/api/v1/oauth/token","token_endpoint_auth_methods_supported":["none"]}
```

### Make the verifier, the challenge and the state

```bash
VERIFIER=$(openssl rand -base64 48 | tr '+/' '-_' | tr -d '=\n')
CHALLENGE=$(printf '%s' "$VERIFIER" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
STATE=$(openssl rand -hex 16)
```

The verifier is 64 characters long.

### Send the user to the consent page

Open this URL in the browser of the user. A real app sends the browser there with a redirect or a link.

```text
http://127.0.0.1:3905/oauth/authorize?response_type=code&client_id=http%3A%2F%2Flocalhost%3A8080&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fcallback&scope=talkgroups%3Aread+lastheard%3Aread&state=<state>&code_challenge=<challenge>&code_challenge_method=S256
```

The consent page checks the request with `GET /api/v1/oauth/authorize` and the same query. You can send that request yourself to check your parameters before you send a user:

```bash
curl -s -G http://127.0.0.1:3905/api/v1/oauth/authorize \
  --data-urlencode response_type=code \
  --data-urlencode client_id=http://localhost:8080 \
  --data-urlencode redirect_uri=http://localhost:8080/callback \
  --data-urlencode 'scope=talkgroups:read lastheard:read' \
  --data-urlencode state=$STATE \
  --data-urlencode code_challenge=$CHALLENGE \
  --data-urlencode code_challenge_method=S256
```

```json
{"origin":"http://localhost:8080","scopes":[{"name":"talkgroups:read","label":"talkgroups","description":"See the talkgroups of this network"},{"name":"lastheard:read","label":"last heard","description":"See last heard for your radio IDs and your hotspots"}]}
```

The user clicks **Allow**, and DMRHub sends the browser to:

```text
http://localhost:8080/callback?code=<code>&iss=http%3A%2F%2F127.0.0.1%3A3905&state=<state>
```

Only the consent page of DMRHub itself can give the answer. A request to `POST /api/v1/oauth/authorize` from another origin gets `403` and `{"error":"access_denied"}`.

### Exchange the code for tokens

```bash
curl -s -i -X POST http://127.0.0.1:3905/api/v1/oauth/token \
  -d grant_type=authorization_code \
  --data-urlencode code=$CODE \
  --data-urlencode redirect_uri=http://localhost:8080/callback \
  --data-urlencode client_id=http://localhost:8080 \
  --data-urlencode code_verifier=$VERIFIER
```

```text
HTTP/1.1 200 OK
Cache-Control: no-store
Content-Type: application/json; charset=utf-8
Pragma: no-cache
X-Rate-Limit-Limit: 50
X-Rate-Limit-Remaining: 44
X-Rate-Limit-Reset: 1793026728

{"access_token":"<access token>","expires_in":900,"refresh_token":"<refresh token>","scope":"talkgroups:read lastheard:read","token_type":"Bearer"}
```

A second exchange of the same code fails:

```json
{"error":"invalid_grant"}
```

### Call the API

```bash
curl -s http://127.0.0.1:3905/api/v1/users/me -H "Authorization: Bearer $ACCESS"
```

```json
{"id":1234567,"callsign":"N0CALL","username":"n0call","admin":false,"superAdmin":false,"email":"","approved":true,"suspended":false,"repeaters":[],"must_change_password":false,"created_at":"2026-10-26T09:58:47.210937579-05:00","sms_storage":false,"position_tracking":false}
```

```bash
curl -s 'http://127.0.0.1:3905/api/v1/lastheard?limit=2' -H "Authorization: Bearer $ACCESS"
```

```json
{"calls":[],"total":0}
```

A route that the scopes do not include:

```bash
curl -s -i http://127.0.0.1:3905/api/v1/messages/inbox -H "Authorization: Bearer $ACCESS"
```

```text
HTTP/1.1 403 Forbidden
Content-Type: application/json; charset=utf-8
Www-Authenticate: Bearer error="insufficient_scope"

{"error":"insufficient_scope"}
```

A browser app on another origin sends a preflight first. DMRHub answers it for any origin:

```bash
curl -s -i -X OPTIONS http://127.0.0.1:3905/api/v1/users/me \
  -H 'Origin: http://localhost:8080' \
  -H 'Access-Control-Request-Method: GET' \
  -H 'Access-Control-Request-Headers: authorization'
```

```text
HTTP/1.1 204 No Content
Access-Control-Allow-Headers: Authorization, Content-Type
Access-Control-Allow-Methods: GET, HEAD, POST, OPTIONS
Access-Control-Allow-Origin: *
Access-Control-Max-Age: 43200
```

### Refresh

```bash
curl -s -X POST http://127.0.0.1:3905/api/v1/oauth/token \
  -d grant_type=refresh_token \
  --data-urlencode refresh_token=$REFRESH \
  --data-urlencode client_id=http://localhost:8080
```

```json
{"access_token":"<new access token>","expires_in":900,"refresh_token":"<new refresh token>","scope":"talkgroups:read lastheard:read","token_type":"Bearer"}
```

Keep the new refresh token and drop the old one. A use of the old refresh token more than 10 seconds later ends the grant:

```json
{"error":"invalid_grant"}
```

After that, the new access token gets `401` and `{"error":"invalid_token"}`, and the new refresh token gets `{"error":"invalid_grant"}`.

### Revoke

```bash
curl -s -i -X POST http://127.0.0.1:3905/api/v1/oauth/revoke \
  --data-urlencode token=$REFRESH \
  --data-urlencode client_id=http://localhost:8080
```

```text
HTTP/1.1 200 OK
Content-Length: 0
```

The revocation endpoint takes an access token or a refresh token, and ends the whole grant. It answers `200` also for a token that it does not know, or that belongs to another app; then it changes nothing. After the revocation:

```text
HTTP/1.1 401 Unauthorized
Content-Type: application/json; charset=utf-8
Www-Authenticate: Bearer error="invalid_token"

{"error":"invalid_token"}
```

The app is no longer in the **Apps** card of the user.

## Websocket

`/ws/messages` sends new messages and positions as they arrive. A browser cannot set the `Authorization` header of a websocket, so offer two subprotocols: `dmrhub`, and `dmrhub.bearer.` followed by the access token. DMRHub picks `dmrhub`.

```js
const ws = new WebSocket('wss://dmrhub.example/ws/messages', ['dmrhub', 'dmrhub.bearer.' + accessToken]);
```

```text
HTTP/1.1 101 Switching Protocols
Sec-Websocket-Protocol: dmrhub
```

- A token with `messages:read` gets message frames, `{"boxes":["inbox"],"message":{...}}`.
- A token with `positions:read` gets position frames, `{"kind":"position","position":{...}}`.
- DMRHub checks the token when the websocket opens. It closes the websocket with code 1008 and a reason, such as `app removed`, when the user removes the app or when the account changes. Then open it again with a fresh access token.

## Errors

### Authorization request

When the client ID or the redirect URI is wrong, DMRHub cannot trust the redirect URI. The consent page then shows an error and does not send the browser back. For every other error, the consent page offers a link back to the redirect URI with `error`, `state` (when the app sent one) and `iss`.

| `error`                     | Cause                                                                                       | Back to the app |
| --------------------------- | ------------------------------------------------------------------------------------------- | --------------- |
| `invalid_client`            | `client_id` is not an origin, uses HTTP on a host that is not loopback, or `redirect_uri` is not on it | No       |
| `invalid_scope`             | No scope, or a scope that is not on the list                                                | Yes             |
| `invalid_request`           | `code_challenge_method` is not `S256`, or `code_challenge` is not a SHA-256 in base64url    | Yes             |
| `unsupported_response_type` | `response_type` is not `code`                                                               | Yes             |
| `access_denied`             | The user clicked **Don't allow**                                                            | Yes             |

`GET /api/v1/oauth/authorize` answers these errors with `400`, as in:

```json
{"error":"invalid_scope","error_description":"invalid_scope: unknown scope \"admin\"","redirect":"http://localhost:8080/callback?error=invalid_scope&iss=http%3A%2F%2F127.0.0.1%3A3905&state=s1"}
```

```json
{"error":"invalid_client","error_description":"authorize: invalid_client: redirect_uri must be on the origin of the app"}
```

### Token endpoint

| Status | `error`                  | Cause                                                                                                                      |
| ------ | ------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| 400    | `invalid_grant`          | The code or refresh token is unknown, expired or spent, or `client_id`, `redirect_uri` or `code_verifier` does not match. A missing code, refresh token, client ID, redirect URI or verifier also gives this error. |
| 400    | `unsupported_grant_type` | `grant_type` is not `authorization_code` or `refresh_token`                                                                |
| 500    | `server_error`           | DMRHub could not save the tokens. Try again later.                                                                         |

When a refresh gives `invalid_grant`, send the user to the consent page again.

### Revocation endpoint

The revocation endpoint answers `200`, or `503` with `{"error":"temporarily_unavailable"}` when DMRHub could not end the grant. Try again later.

### API routes

| Status | Body                                     | Cause                                                                                       |
| ------ | ---------------------------------------- | ------------------------------------------------------------------------------------------- |
| 401    | `{"error":"invalid_token"}`              | The access token is wrong or expired, or its grant ended. Refresh, then try once more.      |
| 403    | `{"error":"insufficient_scope"}`         | The scopes of the token do not include the route, or apps may not use the route.           |
| 403    | `{"error":"...","must_change_password":true}` | An admin set a temporary password for the user. Only `GET /api/v1/users/me` works until the user picks their own password. |
| 429    | `Too many requests. Try again in ...`    | Too many requests from the address. The body is plain text.                                 |

The `401` and `403` token errors also come in the `WWW-Authenticate` header, as in `Bearer error="invalid_token"`.

## Rate limit

DMRHub allows 50 requests each second from one client address, for all of `/api/v1`, `/ws` and the discovery document together. The token and revocation endpoints count too. Each answer has `X-Rate-Limit-Limit`, `X-Rate-Limit-Remaining`, and `X-Rate-Limit-Reset` (a Unix time in seconds). Over the limit, DMRHub answers `429`:

```text
HTTP/1.1 429 Too Many Requests
Content-Type: text/plain; charset=utf-8
X-Rate-Limit-Limit: 50
X-Rate-Limit-Remaining: 0
X-Rate-Limit-Reset: 1793026843

Too many requests. Try again in 658.880437ms
```

The limit is for each address, not for each app or user. Wait until the reset time before you try again.

## Rules for app authors

- Serve the app over HTTPS. DMRHub accepts plain HTTP only on loopback.
- Make a new verifier and a new `state` for each sign-in. Never use the same verifier twice.
- Check that `state` on the redirect is the one you made, and that `iss` is the issuer of the DMRHub you sent the user to. Drop the redirect when either one does not match.
- Exchange the code at once. It works for 2 minutes.
- Ask only for the scopes that the app needs, and ask for all of them each time (see [Ask for every scope at once](#ask-for-every-scope-at-once)).
- Send tokens only in the `Authorization` header, or in the websocket subprotocol. Never put them in a URL, and never write them to logs.
- Keep only the newest refresh token. When several tabs or workers of the app refresh, share the newest refresh token between them. A spent refresh token that comes back after 10 seconds ends the grant.
- Refresh the access token before it expires, or when an API route answers `401`. When the refresh fails with `invalid_grant`, sign the user in again.
- Revoke the refresh token when the user signs out of the app.
- Do not send DMRHub cookies with API requests. A token never gives admin rights, and the routes that are not for apps refuse tokens.
- Slow down on `429`, and do not retry in a tight loop.
