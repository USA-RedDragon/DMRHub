# DMRHub – Copilot Instructions

## Project Overview

DMRHub is a single-binary DMR (Digital Mobile Radio) network server (Go backend + Vue 3 SPA), compatible with MMDVM repeaters. Licensed **AGPL-3.0-or-later** — every `.go` and frontend `.js`/`.mjs` file must carry the SPDX header.

## Architecture

Boot sequence in `cmd/root.go` → config validation (launches setup wizard on failure) → DB + KV + PubSub init → Hub + CallTracker → DMR servers → HTTP server.

| Component          | Location                           | Notes                                                                                                                                            |
| ------------------ | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| MMDVM Server       | `internal/dmr/servers/mmdvm/`      | UDP Homebrew protocol, packet handlers in `packet_handlers.go`                                                                                   |
| OpenBridge         | `internal/dmr/servers/openbridge/` | Peer networking, gated by `config.DMR.OpenBridge.Enabled`                                                                                        |
| Hub (routing core) | `internal/dmr/hub/`                | Protocol-agnostic packet routing. Servers register via `RegisterServer()` with role `RoleRepeater` or `RolePeer`. Central entry: `RoutePacket()` |
| HTTP/API           | `internal/http/`                   | Gin REST API + embedded Vue SPA. Routes in `api/routes.go`, controllers in `api/controllers/v1/{resource}/`                                      |
| Call Tracker       | `internal/dmr/calltracker/`        | Tracks active calls, publishes events via pubsub                                                                                                 |
| PubSub             | `internal/pubsub/`                 | Interface with in-memory and Redis backends. Messages are `[]byte` (msgp or JSON)                                                                |
| KV Store           | `internal/kv/`                     | Interface with TTL support (currently in-memory only)                                                                                            |
| Database           | `internal/db/`                     | GORM — SQLite (default), PostgreSQL, MySQL. Migrations use gormigrate with timestamp IDs (`"YYYYMMDDHHMI"`)                                      |
| Config             | `internal/config/`                 | `configulator` library — YAML files + env vars. Nested structs with `json`/`yaml`/`name`/`default` tags                                          |

### DMR Call Data Flow

Repeater UDP packet → MMDVM `packet_handlers.go` → `models.Packet` (msgp-serialized) → call tracker → pubsub publish → subscription manager fans out to subscribed repeaters → WebSocket clients receive real-time updates.

## Build & Development

```bash
make build          # frontend + backend (CGO_ENABLED=0)
make test           # go test -p 2 -v ./... (CGO_ENABLED=0)
make coverage       # with coverage profile
make lint           # golangci-lint run
go generate ./...   # rebuild msgp code + frontend (npm ci && npm run build)
make update-dbs     # refresh radioid.net user/repeater DBs

# Frontend only (internal/http/frontend/)
npm ci && npm run build    # production
npm run dev                # dev server
npm run test:unit          # vitest
npm run lint               # eslint
```

`CGO_ENABLED=0` is enforced — the binary is fully static (runs in `scratch` Docker container as UID 65534). Frontend is embedded via `//go:embed frontend/dist/*` in `internal/http/server.go`.

## License Header (Required)

Every `.go` file must start with this exact 18-line block:

```go
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
```

## Conventions & Patterns

### API Controllers

- **Naming**: Package-level handler functions named `HTTPMethod + Resource` — `GETRepeaters`, `POSTUser`, `DELETEUser`, `PATCHTalkgroup`
- **Context extraction**: `db, ok := c.MustGet("DB").(*gorm.DB)` — keys are `"DB"`, `"PaginatedDB"`, `"Config"`, `"PubSub"`, `"Hub"`, `"Version"`, `"Commit"`
- **Error responses**: `c.JSON(http.StatusXxx, gin.H{"error": "message"})` — success: `gin.H{"message": "..."}` or `gin.H{"total": count, "items": items}`
- **Auth middleware chain**: `RequireLogin()`, `RequireAdmin()`, `RequireSuperAdmin()`, `RequireRepeaterOwnerOrAdmin()`, `RequireTalkgroupOwnerOrAdmin()`
- **Request binding**: `c.ShouldBindJSON(&json)` with apimodels structs from `internal/http/api/apimodels/`
- **Paginated endpoints**: Use both `"PaginatedDB"` (page query) and `"DB"` (total count)

### Database Models

- **Query functions** are package-level (not repository pattern): `ListRepeaters(db *gorm.DB)`, `FindUserByID(db *gorm.DB, id uint)`, `CountUsers(db *gorm.DB)`
- **User.ID is the DMR Radio ID** (user-supplied uint), NOT auto-increment
- GORM preloads for associations, soft deletes via `gorm.DeletedAt`, `json:"-"` on sensitive fields
- Models with `//go:generate go run github.com/tinylib/msgp` — run `go generate ./...` after modifying these models
- Migration IDs are timestamp strings: `"202302242025"`, `"202602100435"`

### Error Handling

- Sentinel errors as package-level `var` with `Err` prefix: `ErrInvalidLogLevel`, `ErrOpenSocket`, `ErrNoSuchRepeater`
- Always wrap: `fmt.Errorf("descriptive context: %w", err)`
- In controllers: `slog.Error(...)` then `c.JSON(status, gin.H{"error": "..."})` then `return`

### Logging

- Only `log/slog` (structured) — never `fmt.Println` or `log.Printf`. Colorized via `tint`.

### Concurrency

- `puzpuzpuz/xsync/v4` maps for lock-free concurrent data structures (not `sync.Map`)
- `errgroup` for parallel goroutine management with error propagation
- Config import aliased as `configPkg` when ambiguous with local `config` variables

### Testing

- **Every test** uses `t.Parallel()` and **black-box test packages** (`package foo_test`)
- **Assertions**: `github.com/stretchr/testify` — `assert` for soft checks, `require` for hard (fail-fast)
- **HTTP API tests**: `testutils.CreateTestDBRouter()` → in-memory SQLite DB + seeded admin (username `"Admin"`, password `"password"`, callsign `"XXXXXX"`, SuperAdmin+Admin+Approved) + in-memory pubsub → returns `*gin.Engine` for `httptest`
- **Test helpers** in `internal/testutils/`: `RegisterUser`, `LoginUser`, `LoginAdmin`, `CreateAndLoginUser`, `ApproveUser` — manage auth via custom `CookieJar`
- **Response model**: `testutils.APIResponse{Message, Error string}`
- **Hub/integration tests** use `t.Helper()` for helpers, `t.Cleanup()` for teardown, seed data via `db.Create(...)`

## Frontend

Vue 3 SPA in `internal/http/frontend/` — PrimeVue components, Pinia stores (`useUserStore`, `useSettingsStore`), Vue Router, Vite. E2E tests use Cypress, unit tests use Vitest. Frontend `.js`/`.mjs` files also carry the SPDX license header.
