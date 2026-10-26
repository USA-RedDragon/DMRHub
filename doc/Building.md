# Building from Source

This page tells how to build DMRHub and its plugins from source, and how to run the tests. To install a release instead, see the [Deployment Guide](Deployment-Guide.md).

## Prerequisites

| Tool                                                          | Version                                   | Used for                                                                                   |
| ------------------------------------------------------------- | ----------------------------------------- | ------------------------------------------------------------------------------------------ |
| [Go](https://go.dev/doc/install)                              | The `go` line of `go.mod` (1.27.3 now)    | The binary and the Go code generators.                                                     |
| [Node.js](https://nodejs.org/en/download/) and npm            | `.node-version` (24.22.1 now)             | The web frontend, which DMRHub embeds.                                                     |
| [TinyGo](https://tinygo.org/getting-started/install/)         | 0.43.0 or later                           | The built-in plugins. `go generate` runs `tinygo` from the `PATH`, or the command that the `TINYGO` environment variable names. |
| make                                                          | Any                                       | The Makefile targets.                                                                      |
| [golangci-lint](https://golangci-lint.run/)                   | The latest release, as CI uses        | `make lint` only.                                                                          |

## Build with make

```bash
make build
```

`make build` does these steps:

1. `go get`, to download the Go modules.
2. `npm ci` and `npm run build` in `internal/http/frontend`.
3. `go generate ./...`.
4. `CGO_ENABLED=0 go build -o bin/DMRHub`.

The binary is `bin/DMRHub`. It has the frontend and the built-in plugins in it.

## Build by hand

1. Generate the code:

   ```bash
   go generate ./...
   ```

   This step:

   - builds the frontend (`npm ci && npm run build` in `internal/http/frontend`) into `internal/http/frontend/dist`;
   - makes the configuration code with configulator;
   - makes the MessagePack code of the database models with msgp;
   - builds the built-in plugins with TinyGo into `internal/plugins/builtin/builtin.wasm.zst`.

   Git ignores `internal/http/frontend/dist` and `builtin.wasm.zst`. `go build` and `go test` fail without them, so run `go generate ./...` first on a new clone.

1. Build the binary:

   ```bash
   CGO_ENABLED=0 go build -o bin/DMRHub
   ```

1. To run the parrot and the location server as their own programs, build them too:

   ```bash
   CGO_ENABLED=0 go build -o bin/dmrhub-parrot ./plugins/parrot/cmd/parrot
   CGO_ENABLED=0 go build -o bin/dmrhub-locator ./plugins/locator/cmd/locator
   ```

`make run` runs DMRHub from source with `go run -tags codec2wasm .`.

### Codec2

DMRHub converts Codec2 with the WebAssembly build of libcodec2. A build with cgo off, as above, uses it. A build with cgo on, such as `go test -race`, needs `-tags codec2wasm` to use it. Without the tag, it links the system libcodec2, and fails where libcodec2 is not installed.

### Release builds

The releases come from GoReleaser (`.goreleaser.yml`). It builds `DMRHub`, `dmrhub-parrot` and `dmrhub-locator` with `CGO_ENABLED=0` and `-trimpath` for Linux on amd64, arm64, arm (v6 and v7) and riscv64, and for macOS and Windows on amd64 and arm64. The `Dockerfile` only copies those built binaries into the image. It does not build from source.

## Run the tests

Run `go generate ./...` once before the tests.

```bash
make test       # Go tests
make coverage   # Go tests with a coverage profile in coverage.txt
make lint       # golangci-lint
make benchmark  # Go benchmarks
```

`make test` runs `CGO_ENABLED=0 go test -timeout 30m -p 2 -v ./...`, then the tests of `./sdk/plugin/v1/` with `-tags tinygo`.

The database tests run on SQLite, and on PostgreSQL, MySQL and MariaDB in Docker. Without Docker, the tests on the other databases are skipped. The integration tests in `internal/dmr` run on MySQL and MariaDB only when `DMRHUB_TEST_MYSQL` is set:

```bash
DMRHUB_TEST_MYSQL=1 go test -timeout 60m -run '/(mysql|mariadb)$' ./internal/dmr/
```

The race tests in CI need cgo, so they use the Codec2 tag:

```bash
go test -tags codec2wasm -timeout 30m -race ./...
```

`make stress` runs the stress battery in `hack/stress.sh`. The top of that script lists its settings. See [Stress results](stress-results.md) for the results.

## Change the plugin protocol

The plugin protocol is in `proto/dmrhub/plugin/v1/plugin.proto`. After you change it, run:

```bash
make proto
```

`make proto` runs `buf lint` and `buf generate` (buf v1.59.0, through `go run`) and writes the Go code in `sdk/plugin/v1`.

## Frontend development

```bash
cd internal/http/frontend
npm ci
npm run dev                # Vite dev server
npm run test:unit          # Vitest unit tests
npm run lint               # ESLint
npm run build              # type check, sitemap and production build into dist
npm run test:e2e:headless  # Cypress end-to-end tests against npm run preview
```

The Makefile also has `make frontend-unit-test` and `make frontend-e2e-test-chrome` (or `-electron`, `-firefox`).
