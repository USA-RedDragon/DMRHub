THIS_FILE := $(lastword $(MAKEFILE_LIST))

APP_NAME := DMRHub
APP_PATH := github.com/USA-RedDragon/DMRHub

GOFMT_FILES?=$$(find . -name '*.go' | grep -v vendor)

fmt:
	gofmt -w $(GOFMT_FILES)

install-deps:
	@echo "--> Installing Golang dependencies"
	go get
	cd /tmp
	go get github.com/tinylib/msgp/printer@v1.1.8
	go install github.com/tinylib/msgp
	cd -
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

run:
	@echo "--> Running"
	@go run .
	@echo "--> Done"
