THIS_FILE := $(lastword $(MAKEFILE_LIST))

APP_NAME := DMRHub
APP_PATH := github.com/USA-RedDragon/DMRHub

GOFMT_FILES?=$$(find . -name '*.go' | grep -v vendor)

fmt:
	gofmt -w $(GOFMT_FILES)

install-deps:
	@echo "--> Installing Golang dependencies"
	go get
	go install github.com/tinylib/msgp
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
	@go run .
	@echo "--> Done"

coverage:
	@echo "--> Running tests"
	@env CGO_ENABLED=0 go test -coverprofile=coverage.txt -covermode=atomic ./...
	@echo "--> Done"

view-coverage:
	@echo "--> Viewing coverage"
	@go tool cover -html=coverage.txt
	@echo "--> Done"

test:
	@echo "--> Running tests"
	@env CGO_ENABLED=0 go test ./...
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

frontend-e2e-test-electron:
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && env NODE_ENV=test npm run build
	@echo "--> Running end-to-end tests"
	@cd internal/http/frontend && env NODE_ENV=test npm run test:e2e

frontend-e2e-test-chrome:
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && env NODE_ENV=test npm run build
	@echo "--> Running end-to-end tests"
	@cd internal/http/frontend && env NODE_ENV=test npm run test:e2e:chrome

frontend-e2e-test-firefox:
	@cd internal/http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd internal/http/frontend && env NODE_ENV=test npm run build
	@echo "--> Running end-to-end tests"
	@cd internal/http/frontend && env NODE_ENV=test BROWSER=firefox npm run test:e2e:firefox
