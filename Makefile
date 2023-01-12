THIS_FILE := $(lastword $(MAKEFILE_LIST))

APP_NAME := dmrserver-in-a-box
APP_PATH := github.com/USA-RedDragon/dmrserver-in-a-box

GOFMT_FILES?=$$(find . -name '*.go' | grep -v vendor)

fmt:
	gofmt -w $(GOFMT_FILES)

build-frontend:
	@echo "--> Installing JavaScript assets"
	@cd http/frontend && npm ci
	@echo "--> Building Vue application"
	@cd http/frontend && npm run build
	@$(MAKE) -f $(THIS_FILE) fmt

build: build-frontend
	@echo "--> Building"
	@go generate ./...
	@go build -o bin/$(APP_NAME)
	@echo "--> Done"

run:
	@echo "--> Running"
	@go run .
	@echo "--> Done"
