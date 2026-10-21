# Builds the companion the way the release workflow does, so anyone can rebuild a release and get the
# same bytes (VERIFY.md). Go's version is pinned by go.mod's toolchain line.
#
#   make build                      every Windows and Linux file into dist/
#   make reproduce TAG=v0.1.0       rebuild that tag's files and check them against SHA256SUMS
#   make macos VERSION=v0.1.0       on a Mac: the universal binary, the .app and the .dmg (release.yml)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST ?= dist
NAME := gold-forever-companion
APP := Gold Forever Companion
BUNDLE_ID := com.evensberget.gold-forever-companion

# The files players download: Windows's is the one that installs itself on a double-click.
WINDOWS_amd64 := GoldForever-Companion.exe
WINDOWS_arm64 := GoldForever-Companion-arm64.exe

# Windows and Linux need no C compiler; macOS needs cgo for the tray and is built on a Mac.
TARGETS := linux/amd64 linux/arm64 windows/amd64 windows/arm64

# No paths, build id or VCS state in the binary: only the source and the version decide its bytes.
LDFLAGS := -s -w -buildid= -X main.version=$(VERSION)
# Windows builds as a windowed program, so a double-click opens no console window.
WINDOWS_LDFLAGS := $(LDFLAGS) -H=windowsgui

.PHONY: build macos test vet reproduce clean

build:
	@mkdir -p $(DIST)
	@for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; ldflags='$(LDFLAGS)'; out=$(DIST)/$(NAME)-$$os-$$arch; \
		if [ $$os = windows ]; then \
			ldflags='$(WINDOWS_LDFLAGS)'; \
			if [ $$arch = amd64 ]; then out=$(DIST)/$(WINDOWS_amd64); else out=$(DIST)/$(WINDOWS_arm64); fi; \
		fi; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -buildvcs=false -ldflags "$$ldflags" -o $$out ./cmd/$(NAME) || exit 1; \
		echo $$out; \
	done

# The universal binary goes out as it is, for VERIFY.md, and inside the .app, which the .dmg holds.
# The .app's executable keeps the binary's name: the LaunchAgent records the running path.
macos:
	@mkdir -p $(DIST) build-macos
	@for arch in arm64 amd64; do \
		CGO_ENABLED=1 GOOS=darwin GOARCH=$$arch go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o build-macos/$$arch ./cmd/$(NAME) || exit 1; \
	done
	lipo -create -output $(DIST)/$(NAME)-macos-universal build-macos/arm64 build-macos/amd64
	@rm -r -f "build-macos/$(APP).app" && mkdir -p "build-macos/$(APP).app/Contents/MacOS"
	cp $(DIST)/$(NAME)-macos-universal "build-macos/$(APP).app/Contents/MacOS/$(NAME)"
	@short=$$(echo $(VERSION) | sed 's/^v//'); printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0"><dict>' \
		'<key>CFBundleName</key><string>$(APP)</string>' \
		'<key>CFBundleIdentifier</key><string>$(BUNDLE_ID)</string>' \
		'<key>CFBundleExecutable</key><string>$(NAME)</string>' \
		'<key>CFBundlePackageType</key><string>APPL</string>' \
		"<key>CFBundleShortVersionString</key><string>$$short</string>" \
		"<key>CFBundleVersion</key><string>$$short</string>" \
		'<key>LSMinimumSystemVersion</key><string>11.0</string>' \
		'<key>LSUIElement</key><true/>' \
		'</dict></plist>' > "build-macos/$(APP).app/Contents/Info.plist"
	@rm -r -f build-macos/dmg && mkdir build-macos/dmg
	cp -R "build-macos/$(APP).app" build-macos/dmg/
	ln -s /Applications build-macos/dmg/Applications
	hdiutil create -volname "$(APP)" -srcfolder build-macos/dmg -fs HFS+ -format UDZO -ov $(DIST)/GoldForever-Companion.dmg

test:
	go test ./...

vet:
	go vet ./...

# Checks out nothing: run it on a checkout of TAG, with that release's SHA256SUMS in this folder.
reproduce:
	@[ -n "$(TAG)" ] || { echo "usage: make reproduce TAG=v0.1.0 (with the release's SHA256SUMS here)"; exit 2; }
	@$(MAKE) --no-print-directory build VERSION=$(TAG) DIST=reproduce
	@cd reproduce && sha256sum $(NAME)-linux-* GoldForever-Companion*.exe | tee SHA256SUMS.local
	@if [ -f SHA256SUMS ]; then cd reproduce && sha256sum --check --ignore-missing ../SHA256SUMS; \
		else echo "no SHA256SUMS here: download it from the release to compare"; fi

clean:
	rm -r -f dist reproduce build-macos
