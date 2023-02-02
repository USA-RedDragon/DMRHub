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
	@$(MAKE) -f $(THIS_FILE) fmt
	@echo "--> Done"

build: install-deps build-frontend
	@echo "--> Building"
	@go generate ./...
	@go build -o bin/$(APP_NAME)
	@echo "--> Done"

# CI handles the frontend on its own so that
# we don't have to rebuild the frontend on each
# architecture
build-ci: install-deps
	@echo "--> Building"
	@go generate ./...
	@go build -o bin/$(APP_NAME)
	@echo "--> Done"

run:
	@echo "--> Running"
	@go run .
	@echo "--> Done"
