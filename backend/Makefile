.PHONY: build build-all build-frontend clean dev install

BINARY_NAME=opendeploy
VERSION?=0.1.0
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS=-s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)

# Build for current platform
build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/opendeploy

# Build for all supported Linux architectures
build-all: build-linux-arm64 build-linux-amd64 build-linux-arm build-linux-386

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-linux-arm64 ./cmd/opendeploy

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-linux-amd64 ./cmd/opendeploy

build-linux-arm:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-linux-armv7 ./cmd/opendeploy

build-linux-386:
	GOOS=linux GOARCH=386 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-linux-386 ./cmd/opendeploy

# Build the frontend, then embed and compile Go binary
build-release: build-frontend build-linux-arm64
	@echo "Release binary: $(BINARY_NAME)-linux-arm64"

# next.config.js sets distDir to ../backend/static/frontend (static export)
build-frontend:
	cd ../frontend && \
		if [ -f package-lock.json ]; then npm ci; else npm install; fi && \
		npm run build
	@test -f static/frontend/index.html || test -d static/frontend/_next || \
		(echo "Frontend export missing under static/frontend — check next.config.js distDir"; exit 1)

# Dev mode: run with live reload (requires air)
dev:
	go run ./cmd/opendeploy --config config.example.yaml

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux-*
	rm -rf static/frontend

# Detect host arch for install (matches scripts/install.sh)
HOST_ARCH := $(shell uname -m)
ifeq ($(HOST_ARCH),aarch64)
  INSTALL_SUFFIX := linux-arm64
else ifeq ($(HOST_ARCH),arm64)
  INSTALL_SUFFIX := linux-arm64
else ifeq ($(HOST_ARCH),x86_64)
  INSTALL_SUFFIX := linux-amd64
else ifeq ($(HOST_ARCH),armv7l)
  INSTALL_SUFFIX := linux-armv7
else ifeq ($(HOST_ARCH),i686)
  INSTALL_SUFFIX := linux-386
else
  INSTALL_SUFFIX := linux-$(HOST_ARCH)
endif

# Install on the target system (runs as root — matches opendeploy.service User=root)
# Prefer scripts/install.sh for full OS dependency bootstrap.
install:
	@test -f $(BINARY_NAME)-$(INSTALL_SUFFIX) || test -f $(BINARY_NAME) || \
		(echo "Missing binary $(BINARY_NAME)-$(INSTALL_SUFFIX). Run: make build-linux-arm64|build-linux-amd64|build-linux-arm|build-linux-386 or make build"; exit 1)
	@if [ -f $(BINARY_NAME)-$(INSTALL_SUFFIX) ]; then \
		install -m 755 $(BINARY_NAME)-$(INSTALL_SUFFIX) /usr/local/bin/opendeploy; \
	else \
		install -m 755 $(BINARY_NAME) /usr/local/bin/opendeploy; \
	fi
	install -m 644 opendeploy.service /etc/systemd/system/opendeploy.service
	mkdir -p /etc/opendeploy /var/lib/opendeploy /var/log/opendeploy /var/www/opendeploy
	test -f /etc/opendeploy/config.yaml || install -m 644 config.example.yaml /etc/opendeploy/config.yaml
	chown -R root:root /var/lib/opendeploy /var/log/opendeploy /etc/opendeploy
	systemctl daemon-reload
	systemctl enable opendeploy
	@echo "Installed ($(INSTALL_SUFFIX)). Start with: systemctl start opendeploy"
	@echo "For full Pi bootstrap (apt deps + LXD): sudo ../scripts/install.sh --binary /usr/local/bin/opendeploy"
