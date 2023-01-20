# Project variables
PROJECT_NAME := xelon-csi
IMAGE_NAME := xelonag/xelon-csi

# Build variables
.DEFAULT_GOAL = test
BUILD_DIR := build

VERSION ?= $(shell git describe --always)
COMMIT ?= $(shell git rev-parse HEAD)
ifeq ($(strip $(shell git status --porcelain 2>/dev/null)),)
  GIT_TREE_STATE=clean
else
  GIT_TREE_STATE=dirty
endif
BUILD_DATE ?= $(shell date -Is)
LDFLAGS ?= -X github.com/Xelon-AG/xelon-csi/driver.driverVersion=${VERSION}
LDFLAGS := $(LDFLAGS) -X github.com/Xelon-AG/xelon-csi/driver.gitCommit=${COMMIT}
LDFLAGS := $(LDFLAGS) -X github.com/Xelon-AG/xelon-csi/driver.gitTreeState=${GIT_TREE_STATE}
LDFLAGS := $(LDFLAGS) -X github.com/Xelon-AG/xelon-csi/driver.buildDate=${BUILD_DATE}


## tools: Install required tooling.
.PHONY: tools
tools:
	@echo "==> Installing required tooling..."
	@cd tools && go install github.com/golangci/golangci-lint/cmd/golangci-lint


## clean: Delete the build directory.
.PHONY: clean
clean:
	@echo "==> Removing '$(BUILD_DIR)' directory..."
	@rm -rf $(BUILD_DIR)


## lint: Lint code with golangci-lint.
.PHONY: lint
lint:
	@echo "==> Linting code with 'golangci-lint'..."
	@golangci-lint run ./...


## test: Run all unit tests.
.PHONY: test
test:
	@echo "==> Running unit tests..."
	@mkdir -p $(BUILD_DIR)
	@go test -count=1 -v -cover -coverprofile=$(BUILD_DIR)/coverage.out -parallel=4 ./...


## build: Build binary for linux/amd64 system.
.PHONE: build
build:
	@echo "==> Building binary..."
	@echo "    running go build for GOOS=linux GOARCH=amd64"
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(PROJECT_NAME) cmd/xelon-csi/main.go


## build-docker: Build docker image with included binary.
.PHONE: build-docker-dev
build-docker-dev: build
	@echo "==> Building docker image $(IMAGE_NAME)..."
	@docker build -t $(IMAGE_NAME) -f Dockerfile.dev .


## release-docker-dev: Release development docker image.
.PHONE: release-docker-dev
release-docker-dev: build-docker-dev
	@echo "==> Releasing development docker image $(IMAGE_NAME):dev..."
	@docker image tag $(IMAGE_NAME) $(IMAGE_NAME):dev
	@docker push $(IMAGE_NAME):dev


help: Makefile
	@echo "Usage: make <command>"
	@echo ""
	@echo "Commands:"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'
