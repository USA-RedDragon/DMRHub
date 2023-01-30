THIS_FILE := $(lastword $(MAKEFILE_LIST))

APP_NAME := dmrserver-in-a-box
APP_PATH := github.com/USA-RedDragon/dmrserver-in-a-box

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
	@cd http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd http/frontend && npm run build
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
