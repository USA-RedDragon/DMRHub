THIS_FILE := $(lastword $(MAKEFILE_LIST))

APP_NAME := DMRHub
APP_PATH := github.com/USA-RedDragon/DMRHub

GOFMT_FILES?=$$(find . -name '*.go' | grep -v vendor)

fmt:
	gofmt -w $(GOFMT_FILES)

install-deps:
	@echo "--> Installing Golang dependencies"
	go get
	@echo "--> Done"

build-frontend:
	@echo "--> Installing JavaScript assets"
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && npm run build
	@echo "--> Done"

build: install-deps build-frontend
	@echo "--> Building"
	@go generate ./...
	@env CGO_ENABLED=0 go build -o bin/$(APP_NAME)
	@echo "--> Done"

# CI handles the frontend on its own so that
# we don't have to rebuild the frontend on each
# architecture
build-ci: install-deps
	@echo "--> Building"
	@go generate ./...
	@env CGO_ENABLED=0 go build -o bin/$(APP_NAME)
	@echo "--> Done"

run:
	@echo "--> Running"
	@go run -tags codec2wasm .
	@echo "--> Done"

coverage:
	@echo "--> Running tests"
	@env CGO_ENABLED=0 go test -timeout 30m -v -coverprofile=coverage.txt -coverpkg=./... -covermode=atomic ./...
	@echo "--> Done"

view-coverage:
	@echo "--> Viewing coverage"
	@go tool cover -html=coverage.txt
	@echo "--> Done"

.PHONY: proto
proto:
	@echo "--> Generating the plugin protocol"
	@go run github.com/bufbuild/buf/cmd/buf@v1.59.0 lint proto
	@go run github.com/bufbuild/buf/cmd/buf@v1.59.0 generate
	@awk '/^\/\/ Code generated/ && !done { print "//go:build !wasip1"; print ""; done = 1 } { print }' sdk/plugin/v1/plugin_grpc.pb.go > sdk/plugin/v1/plugin_grpc.pb.go.tmp
	@mv sdk/plugin/v1/plugin_grpc.pb.go.tmp sdk/plugin/v1/plugin_grpc.pb.go
	@awk '/^\/\/ Code generated/ && !done { print "//go:build !tinygo"; print ""; done = 1 } { print }' sdk/plugin/v1/plugin.pb.go > sdk/plugin/v1/plugin.pb.go.tmp
	@mv sdk/plugin/v1/plugin.pb.go.tmp sdk/plugin/v1/plugin.pb.go
	@go run github.com/bufbuild/buf/cmd/buf@v1.59.0 generate --template buf.gen.lite.yaml
	@mv sdk/plugin/v1/lite.tmp/dmrhub/plugin/v1/plugin.pb.go sdk/plugin/v1/plugin_lite.pb.go
	@rm -r sdk/plugin/v1/lite.tmp
	@go run github.com/bufbuild/buf/cmd/buf@v1.59.0 build proto --exclude-source-info --as-file-descriptor-set -o sdk/plugin/v1/testdata/plugin.binpb
	@echo "--> Done"

lint:
	@echo "--> Linting"
	@golangci-lint run
	@echo "--> Done"

test:
	@echo "--> Running tests"
	@env CGO_ENABLED=0 go test -timeout 30m -p 2 -v ./...
	@env CGO_ENABLED=0 go test -tags tinygo -v ./sdk/plugin/v1/
	@echo "--> Done"

stress:
	@echo "--> Running the stress battery (see hack/stress.sh for its settings)"
	@./hack/stress.sh
	@echo "--> Done"

screenshots:
	@echo "--> Making the screenshots of the docs (see hack/screenshots.sh for its settings)"
	@./hack/screenshots.sh
	@echo "--> Done"

docs-site:
	@echo "--> Building the docs site into artifacts/docs-site/index.html"
	@mkdir -p artifacts/docs-site
	@python3 hack/docs-site/build.py doc artifacts/docs-site/index.html
	@echo "--> Done"

benchmark:
	@echo "--> Running benchmarks"
	@env CGO_ENABLED=0 go test -run ^$ -benchmem -bench=. ./...
	@echo "--> Done"

update-dbs: update-repeaterdb update-userdb

update-userdb:
	@echo "--> Updating user database"
	wget -O internal/userdb/users.json https://www.radioid.net/static/users.json
	@rm -f internal/userdb/users.json.xz
	@cd internal/userdb && xz -e users.json
	@date --rfc-3339=seconds | sed 's/ /T/' > internal/userdb/userdb-date.txt.tmp
	@tr -d '\n' < internal/userdb/userdb-date.txt.tmp > internal/userdb/userdb-date.txt
	@rm -f internal/userdb/userdb-date.txt.tmp
	@echo "--> Done"

update-repeaterdb:
	@echo "--> Updating repeater database"
	wget -O internal/repeaterdb/repeaters.json https://www.radioid.net/static/rptrs.json
	@rm -f internal/repeaterdb/repeaters.json.xz
	@cd internal/repeaterdb && xz -e repeaters.json
	@date --rfc-3339=seconds | sed 's/ /T/' > internal/repeaterdb/repeaterdb-date.txt.tmp
	@tr -d '\n' < internal/repeaterdb/repeaterdb-date.txt.tmp > internal/repeaterdb/repeaterdb-date.txt
	@rm -f internal/repeaterdb/repeaterdb-date.txt.tmp
	@echo "--> Done"

frontend-unit-test:
	@cd internal/http/frontend && npm run test:unit

frontend-unit-test-coverage:
	@cd internal/http/frontend && npm run test:unit:coverage

frontend-e2e-test-electron:
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && env NODE_ENV=test CYPRESS_COVERAGE=true npm run build
	@echo "--> Running end-to-end tests"
	@cd internal/http/frontend && env NODE_ENV=test CYPRESS_COVERAGE=true npm run test:e2e

frontend-e2e-test-chrome:
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && env NODE_ENV=test CYPRESS_COVERAGE=true npm run build
	@echo "--> Running end-to-end tests"
	@cd internal/http/frontend && env NODE_ENV=test CYPRESS_COVERAGE=true npm run test:e2e:chrome

frontend-e2e-test-firefox:
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && env NODE_ENV=test CYPRESS_COVERAGE=true npm run build
	@echo "--> Running end-to-end tests"
	@cd internal/http/frontend && env NODE_ENV=test CYPRESS_COVERAGE=true BROWSER=firefox npm run test:e2e:firefox
