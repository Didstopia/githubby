SHELL := /bin/bash

export GO111MODULE=on

export PATH := $(GOPATH)/bin:$(PATH)

BINARY_VERSION?=0.0.1
BINARY_OUTPUT?=githubby
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-X main.Version=$(BINARY_VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)"

define timed_function
	@d=$$(date +%s); \
	$(shell echo $1); \
	echo "=> Ran $1 in $$(($$(date +%s)-d)) seconds"
endef

.PHONY: all install uninstall build test clean deps upgrade tidy lint vet

all: deps build

install:
	$(call timed_function,'go install -v $(LDFLAGS)')

uninstall:
	$(call timed_function,'rm -f $(GOPATH)/bin/$(BINARY_OUTPUT)')

build:
	$(call timed_function,'go build -v $(LDFLAGS) -o $(BINARY_OUTPUT)')

test:
	$(call timed_function,'go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...')

clean:
	$(call timed_function,'go clean')
	$(call timed_function,'rm -f $(BINARY_OUTPUT)')
	$(call timed_function,'rm -f coverage.txt')

deps:
	$(call timed_function,'go mod download')
	$(call timed_function,'go build -v ./...')

upgrade:
	$(call timed_function,'go get -u ./...')
	$(call timed_function,'go mod tidy')

tidy:
	$(call timed_function,'go mod tidy')

vet:
	$(call timed_function,'go vet ./...')

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi
