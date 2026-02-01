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

build-frontend:
	cd ../frontend && npm ci && npm run build
	rm -rf static/frontend
	cp -r ../frontend/.next/static static/frontend 2>/dev/null || cp -r ../frontend/out static/frontend 2>/dev/null || true

# Dev mode: run with live reload (requires air)
dev:
	go run ./cmd/opendeploy --config config.example.yaml

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux-*
	rm -rf static/frontend

# Install on the target system
install:
	install -m 755 $(BINARY_NAME)-linux-arm64 /usr/local/bin/opendeploy
	install -m 644 opendeploy.service /etc/systemd/system/opendeploy.service
	mkdir -p /etc/opendeploy /var/lib/opendeploy /var/log/opendeploy
	test -f /etc/opendeploy/config.yaml || install -m 644 config.example.yaml /etc/opendeploy/config.yaml
	id -u opendeploy &>/dev/null || useradd -r -s /sbin/nologin opendeploy
	chown -R opendeploy:opendeploy /var/lib/opendeploy /var/log/opendeploy
	systemctl daemon-reload
	systemctl enable opendeploy
	@echo "Installed. Start with: systemctl start opendeploy"
