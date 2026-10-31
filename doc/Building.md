# Building from Source

This page tells how to build DMRHub and its plugins from source, and how to run the tests. To install a release instead, see [Install DMRHub](admin/install.md).

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

`make stress` runs the stress battery in `hack/stress.sh`. The top of that script lists its settings. See [Stress results](admin/stress-results.md) for the results.

## Build the docs site

The docs site at dmrhub.net has one page for each Markdown file of `doc/`. The DMRHub docs home is the site home at `/`, and the other pages are at `/docs/<page>/`, such as `/docs/getting-started/` for `doc/getting-started.md`. The docs of [dmrgo](https://github.com/USA-RedDragon/dmrgo), a DMRHub project, are at `/docs/dmrgo/`.

`hack/docs-site/build.py` makes the site with pandoc. These files control it:

- `doc/nav.md` is the navigation, in reading order. The build stops when it names a page that `doc/` does not have. A Markdown file that `doc/nav.md` does not name still gets a page, but no navigation item.
- `hack/docs-site/home.html` is the content of the home page.
- `hack/docs-site/shell/` holds the page frame (`page.html`), its CSS and script, and the fonts. The frame has the header, the theme setting and the colors of the DMRHub web interface.

The build stops when a page links to a page or an anchor that does not exist. It also writes `/docs/sitemap.xml`, with the date of the last commit of each page when the source tree is a git repository.

It needs Python 3 and pandoc. Run:

```bash
make docs-site
```

The site is in `artifacts/docs-site`. To add the dmrgo docs, give a dmrgo source tree: `python3 hack/docs-site/build.py --dmrgo <dmrgo directory>`. The site uses absolute paths, so serve the directory from the root of a web server to view it.

`hack/docs-site/Dockerfile` builds the image that serves the site at dmrhub.net. It clones dmrgo at the ref in the `DMRGO_REF` build argument (default `main`). To use a local dmrgo tree instead, give it as the `dmrgo` build context:

```bash
docker build -f hack/docs-site/Dockerfile --build-arg DMRGO_REF=main -t dmrhub-docs .
docker build -f hack/docs-site/Dockerfile --build-context dmrgo=../dmrgo -t dmrhub-docs .
```

The image runs `hack/docs-site/server` on port 8080 as an unprivileged user. It redirects `/docs/` to `/` and a page address without its trailing slash to the address with it, and sends the zstd copy of a file to a browser that accepts zstd.

## Make the screenshots of the docs

Each image in `doc/Screenshots` comes from the screenshot harness. `hack/screenshots/shots.yaml` lists each shot: its file name, the page, the user that is logged in, the state of the demo network that it needs, and the clicks and text to enter before the capture. Run:

```bash
make screenshots
```

`hack/screenshots.sh` builds DMRHub, runs it on loopback with SQLite, fills it with a demo network (users, talkgroups, hotspots that log in over the MMDVM protocol, calls, text messages and positions), and takes each shot with headless Chromium. It needs Go, npm, TinyGo 0.43.0 or later, `unshare`, `pasta`, Chromium and `oxipng`. The top of the script lists its settings.

The output does not change from run to run while the UI stays the same:

- DMRHub and the browser run on a clock that the harness sets. The clock starts at `start` in `shots.yaml` and moves on by a fixed time for each shot, call and position.
- The demo data, its IDs and the passwords are the same in each run. The harness replaces each password, token and key that DMRHub makes at random with a fixed example value before the capture.
- The fonts come with the frontend. The map uses the basemap archive that `shots.yaml` names.
- The browser turns off animations and transitions.

To add a screenshot, add a shot to `shots.yaml` and refer to it from a doc as `Screenshots/<name>.png`. `go run ./hack/screenshots -phase check` fails when a doc refers to an image that the harness does not make, when a doc still has a screenshot placeholder comment, or when `doc/Screenshots` has a file that the harness does not make. The Screenshots workflow runs this check on each push and pull request, and makes the screenshots again on each release tag.

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
