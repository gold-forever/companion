# Builds the companion the way the release workflow does, so anyone can rebuild a release and get the
# same bytes (VERIFY.md). Go's version is pinned by go.mod's toolchain line.
#
#   make build                      every Windows and Linux binary into dist/
#   make reproduce TAG=v0.1.0       rebuild that tag's binaries and check them against SHA256SUMS

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST ?= dist
NAME := gold-forever-companion

# Windows and Linux need no C compiler; macOS is built on a Mac (release.yml), for its tray later.
TARGETS := linux/amd64 linux/arm64 windows/amd64 windows/arm64

# No paths, build id or VCS state in the binary: only the source and the version decide its bytes.
LDFLAGS := -s -w -buildid= -X main.version=$(VERSION)
# Windows builds as a windowed program, so a double-click opens no console window.
WINDOWS_LDFLAGS := $(LDFLAGS) -H=windowsgui

.PHONY: build test vet reproduce clean

build:
	@mkdir -p $(DIST)
	@for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; ext=; ldflags='$(LDFLAGS)'; \
		[ $$os = windows ] && { ext=.exe; ldflags='$(WINDOWS_LDFLAGS)'; }; \
		out=$(DIST)/$(NAME)-$$os-$$arch$$ext; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -buildvcs=false -ldflags "$$ldflags" -o $$out ./cmd/$(NAME) || exit 1; \
		echo $$out; \
	done

test:
	go test ./...

vet:
	go vet ./...

# Checks out nothing: run it on a checkout of TAG, with that release's SHA256SUMS in this folder.
reproduce:
	@[ -n "$(TAG)" ] || { echo "usage: make reproduce TAG=v0.1.0 (with the release's SHA256SUMS here)"; exit 2; }
	@$(MAKE) --no-print-directory build VERSION=$(TAG) DIST=reproduce
	@cd reproduce && sha256sum $(NAME)-* | tee SHA256SUMS.local
	@if [ -f SHA256SUMS ]; then cd reproduce && sha256sum --check --ignore-missing ../SHA256SUMS; \
		else echo "no SHA256SUMS here: download it from the release to compare"; fi

clean:
	rm -r -f dist reproduce
