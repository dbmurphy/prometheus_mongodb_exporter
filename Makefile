.PHONY: all build clean default help init test format check-license
default: help

GO_TEST_PATH ?= ./...
GO_TEST_EXTRA ?=
GO_TEST_COVER_PROFILE ?= cover.out
GO_TEST_CODECOV ?=

BUILD ?= $(shell date +%FT%T%z)
GOVERSION ?= $(shell go version | cut -d " " -f3)
COMPONENT_VERSION ?= $(shell git describe --abbrev=0 --always)
COMPONENT_BRANCH ?= $(shell git describe --always --contains --all)
PMM_RELEASE_FULLCOMMIT ?= $(shell git rev-parse HEAD)
GO_BUILD_LDFLAGS = -X main.version=${COMPONENT_VERSION} -X main.buildDate=${BUILD} -X main.commit=${PMM_RELEASE_FULLCOMMIT} -X main.Branch=${COMPONENT_BRANCH} -X main.GoVersion=${GOVERSION} -s -w
NAME ?= mongodb_exporter
REPO ?= percona/$(NAME)
GORELEASER_FLAGS ?=
UID ?= $(shell id -u)

export TEST_MONGODB_IMAGE?=mongo:5.0
export TEST_MONGODB_ADMIN_USERNAME?=
export TEST_MONGODB_ADMIN_PASSWORD?=
export TEST_MONGODB_USERNAME?=
export TEST_MONGODB_PASSWORD?=
export TEST_MONGODB_S1_RS?=rs1
export TEST_MONGODB_STANDALONE_PORT?=27017
export TEST_MONGODB_S1_PRIMARY_PORT?=17001
export TEST_MONGODB_S1_SECONDARY1_PORT?=17002
export TEST_MONGODB_S1_SECONDARY2_PORT?=17003
export TEST_MONGODB_S1_ARBITER_PORT?=17011
export TEST_MONGODB_S2_RS?=rs2
export TEST_MONGODB_S2_PRIMARY_PORT?=17004
export TEST_MONGODB_S2_SECONDARY1_PORT?=17005
export TEST_MONGODB_S2_SECONDARY2_PORT?=17006
export TEST_MONGODB_S2_ARBITER_PORT?=17012
export TEST_MONGODB_CONFIGSVR_RS?=csReplSet
export TEST_MONGODB_CONFIGSVR1_PORT?=17007
export TEST_MONGODB_CONFIGSVR2_PORT?=17008
export TEST_MONGODB_CONFIGSVR3_PORT?=17009
export TEST_MONGODB_MONGOS_PORT?=17000
export PMM_RELEASE_PATH?=.

define TEST_ENV
	TEST_MONGODB_ADMIN_USERNAME=$(TEST_MONGODB_ADMIN_USERNAME) \
	TEST_MONGODB_ADMIN_PASSWORD=$(TEST_MONGODB_ADMIN_PASSWORD) \
	TEST_MONGODB_USERNAME=$(TEST_MONGODB_USERNAME) \
	TEST_MONGODB_PASSWORD=$(TEST_MONGODB_PASSWORD) \
	TEST_MONGODB_S1_RS=$(TEST_MONGODB_S1_RS) \
	TEST_MONGODB_STANDALONE_PORT=$(TEST_MONGODB_STANDALONE_PORT) \
	TEST_MONGODB_S1_PRIMARY_PORT=$(TEST_MONGODB_S1_PRIMARY_PORT) \
	TEST_MONGODB_S1_SECONDARY1_PORT=$(TEST_MONGODB_S1_SECONDARY1_PORT) \
	TEST_MONGODB_S1_SECONDARY2_PORT=$(TEST_MONGODB_S1_SECONDARY2_PORT) \
	TEST_MONGODB_S1_ARTBITER_PORT=$(TEST_MONGODB_S1_ARBITER_PORT) \
	TEST_MONGODB_S2_RS=$(TEST_MONGODB_S2_RS) \
	TEST_MONGODB_S2_PRIMARY_PORT=$(TEST_MONGODB_S2_PRIMARY_PORT) \
	TEST_MONGODB_S2_SECONDARY1_PORT=$(TEST_MONGODB_S2_SECONDARY1_PORT) \
	TEST_MONGODB_S2_SECONDARY2_PORT=$(TEST_MONGODB_S2_SECONDARY2_PORT) \
	TEST_MONGODB_S2_ARTBITER_PORT=$(TEST_MONGODB_S2_ARBITER_PORT) \
	TEST_MONGODB_CONFIGSVR_RS=$(TEST_MONGODB_CONFIGSVR_RS) \
	TEST_MONGODB_CONFIGSVR1_PORT=$(TEST_MONGODB_CONFIGSVR1_PORT) \
	TEST_MONGODB_CONFIGSVR2_PORT=$(TEST_MONGODB_CONFIGSVR2_PORT) \
	TEST_MONGODB_CONFIGSVR3_PORT=$(TEST_MONGODB_CONFIGSVR3_PORT) \
	TEST_MONGODB_MONGOS_PORT=$(TEST_MONGODB_MONGOS_PORT) \
	TEST_MONGODB_IMAGE=$(TEST_MONGODB_IMAGE)
endef

env:
	@echo $(TEST_ENV) | tr ' ' '\n' >.env

init:                       ## Install linters.
	cd tools && go generate -x -tags=tools

build:                      ## Compile using plain go build
	go build -ldflags="$(GO_BUILD_LDFLAGS)"  -o $(PMM_RELEASE_PATH)/mongodb_exporter

release:                      ## Build the binaries using goreleaser
	docker run --rm --privileged \
		-v ${PWD}:/go/src/github.com/user/repo \
		-w /go/src/github.com/user/repo \
		goreleaser/goreleaser release --snapshot --skip-publish --rm-dist 

FILES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

format:                     ## Format source code.
	go mod tidy
	bin/gofumpt -l -w $(FILES)
	bin/gci write --Section Standard --Section Default --Section "Prefix(github.com/percona/mongodb_exporter)" .

check:                      ## Run checks/linters
	bin/golangci-lint run

check-license:              ## Check license in headers.
	@go run .github/check-license.go

help:                       ## Display this help message.
	@echo "Please use \`make <target>\` where <target> is one of:"
	@grep '^[a-zA-Z]' $(MAKEFILE_LIST) | \
	awk -F ':.*?## ' 'NF==2 {printf "  %-26s%s\n", $$1, $$2}'

test: env                   ## Run all tests.
	go test -v -count 1 -timeout 30s ./...

test-race: env              ## Run all tests with race flag.
	go test -race -v -timeout 30s ./...

test-cluster: env           ## Starts MongoDB test cluster. Use env var TEST_MONGODB_IMAGE to set flavor and version. Example: TEST_MONGODB_IMAGE=mongo:3.6 make test-cluster
	docker-compose up -d

test-cluster-clean: env     ## Stops MongoDB test cluster.
	docker-compose down --remove-orphans
