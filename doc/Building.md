# Building from Source

DMRHub can be built using the Makefile or manually.

## Prerequisites

- [Go](https://go.dev/doc/install) (see `go.mod` for the minimum version)
- [Node.js](https://nodejs.org/en/download/) (for the frontend)
- [TinyGo](https://tinygo.org/getting-started/install/) 0.43.0 or later (for the plugins that come with DMRHub, and for the tests). `go generate` runs `tinygo` from the `PATH`, or the one that the `TINYGO` environment variable names
- Make sure `$GOPATH/bin` is in your `$PATH`

## With Make

```bash
make build
```

The binary is output to `bin/DMRHub`.

## Manually

1. **Generate Go code** (MessagePack serializers, embedded frontend, the plugins that come with DMRHub):

   ```bash
   go generate ./...
   ```

1. **Build the binary:**

   ```bash
   CGO_ENABLED=0 go build -o bin/DMRHub
   ```

## Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make coverage

# Lint
make lint
```

Tests run with `CGO_ENABLED=0` and parallelism limited to 2.

DMRHub converts Codec2 with the WebAssembly build of libcodec2. A build with cgo on, such as `go test -race`, needs `-tags codec2wasm` to use it; without the tag it links the system libcodec2 and fails where that is not installed.

## Frontend Development

```bash
cd internal/http/frontend
npm ci
npm run dev          # Vite dev server
npm run test:unit    # Vitest unit tests
npm run lint         # ESLint
```
