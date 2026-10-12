.PHONY: build package package-linux-amd64 package-linux-arm64 build-x86 build-arm prepare-package build-web build-agent-downloads stage-linux-amd64 stage-linux-arm64 archive-linux-amd64 archive-linux-arm64 build-server-linux-amd64 build-server-linux-arm64 clean

DIST_DIR := dist
PACKAGE_NAME := mizupanel
COMMON_DIR := $(DIST_DIR)/package-common
COMMON_DOWNLOAD_DIR := $(COMMON_DIR)/downloads
COMMON_WEB_DIR := $(COMMON_DIR)/web
PREPARED := $(DIST_DIR)/.prepared

AMD64 := linux-amd64
ARM64 := linux-arm64
AMD64_DIR := $(DIST_DIR)/$(PACKAGE_NAME)-$(AMD64)
ARM64_DIR := $(DIST_DIR)/$(PACKAGE_NAME)-$(ARM64)
AMD64_TARBALL := $(DIST_DIR)/$(PACKAGE_NAME)-$(AMD64).tar.gz
ARM64_TARBALL := $(DIST_DIR)/$(PACKAGE_NAME)-$(ARM64).tar.gz

AMD64_CC ?= gcc
ARM64_CC ?= aarch64-linux-gnu-gcc

build: package-linux-amd64

package: package-linux-amd64

build-x86: package-linux-amd64

build-arm: package-linux-arm64

prepare-package: $(PREPARED)

$(PREPARED):
	rm -rf $(COMMON_DIR) $(AMD64_DIR) $(ARM64_DIR) $(AMD64_TARBALL) $(ARM64_TARBALL)
	mkdir -p $(COMMON_DIR)
	touch $(PREPARED)

build-web: $(PREPARED)
	if [ "$(SKIP_WEB_BUILD)" != "1" ]; then npm --prefix web run build; fi
	test -f web/dist/index.html
	rm -rf $(COMMON_WEB_DIR)
	mkdir -p $(COMMON_WEB_DIR)
	cp -R web/dist/. $(COMMON_WEB_DIR)/

build-agent-downloads: $(PREPARED)
	rm -rf $(COMMON_DOWNLOAD_DIR)
	mkdir -p $(COMMON_DOWNLOAD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(COMMON_DOWNLOAD_DIR)/mizupanel-agent-linux-amd64 ./cmd/agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(COMMON_DOWNLOAD_DIR)/mizupanel-agent-linux-arm64 ./cmd/agent
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(COMMON_DOWNLOAD_DIR)/mizupanel-agent-windows-amd64.exe ./cmd/agent

stage-linux-amd64: $(PREPARED) build-web build-agent-downloads
	rm -rf $(AMD64_DIR) $(AMD64_TARBALL)
	mkdir -p $(AMD64_DIR)/data $(AMD64_DIR)/scripts $(AMD64_DIR)/systemd $(AMD64_DIR)/downloads $(AMD64_DIR)/web
	cp VERSION $(AMD64_DIR)/VERSION
	cp examples/server.example.yaml $(AMD64_DIR)/server.example.yaml
	cp scripts/install-agent.sh $(AMD64_DIR)/scripts/install-agent.sh
	cp scripts/install-agent.ps1 $(AMD64_DIR)/scripts/install-agent.ps1
	cp scripts/uninstall-agent.sh $(AMD64_DIR)/scripts/uninstall-agent.sh
	cp scripts/uninstall-agent.ps1 $(AMD64_DIR)/scripts/uninstall-agent.ps1
	cp systemd/*.service $(AMD64_DIR)/systemd/
	cp -R $(COMMON_DOWNLOAD_DIR)/. $(AMD64_DIR)/downloads/
	cp -R $(COMMON_WEB_DIR)/. $(AMD64_DIR)/web/

stage-linux-arm64: $(PREPARED) build-web build-agent-downloads
	rm -rf $(ARM64_DIR) $(ARM64_TARBALL)
	mkdir -p $(ARM64_DIR)/data $(ARM64_DIR)/scripts $(ARM64_DIR)/systemd $(ARM64_DIR)/downloads $(ARM64_DIR)/web
	cp VERSION $(ARM64_DIR)/VERSION
	cp examples/server.example.yaml $(ARM64_DIR)/server.example.yaml
	cp scripts/install-agent.sh $(ARM64_DIR)/scripts/install-agent.sh
	cp scripts/install-agent.ps1 $(ARM64_DIR)/scripts/install-agent.ps1
	cp scripts/uninstall-agent.sh $(ARM64_DIR)/scripts/uninstall-agent.sh
	cp scripts/uninstall-agent.ps1 $(ARM64_DIR)/scripts/uninstall-agent.ps1
	cp systemd/*.service $(ARM64_DIR)/systemd/
	cp -R $(COMMON_DOWNLOAD_DIR)/. $(ARM64_DIR)/downloads/
	cp -R $(COMMON_WEB_DIR)/. $(ARM64_DIR)/web/

package-linux-amd64: archive-linux-amd64

package-linux-arm64: archive-linux-arm64

build-server-linux-amd64: stage-linux-amd64
	@command -v $(AMD64_CC) >/dev/null || (echo "missing AMD64_CC=$(AMD64_CC); install gcc or pass AMD64_CC=/path/to/gcc" >&2; exit 1)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=$(AMD64_CC) go build -o $(AMD64_DIR)/mizupanel-server ./cmd/server

build-server-linux-arm64: stage-linux-arm64
	@command -v $(ARM64_CC) >/dev/null || (echo "missing ARM64_CC=$(ARM64_CC); install gcc-aarch64-linux-gnu or pass ARM64_CC=/path/to/aarch64-linux-gnu-gcc" >&2; exit 1)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=$(ARM64_CC) go build -o $(ARM64_DIR)/mizupanel-server ./cmd/server

archive-linux-amd64: build-server-linux-amd64
	cd $(DIST_DIR) && tar -czf $(PACKAGE_NAME)-$(AMD64).tar.gz $(PACKAGE_NAME)-$(AMD64)

archive-linux-arm64: build-server-linux-arm64
	cd $(DIST_DIR) && tar -czf $(PACKAGE_NAME)-$(ARM64).tar.gz $(PACKAGE_NAME)-$(ARM64)

clean:
	rm -rf $(DIST_DIR)
