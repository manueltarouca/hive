# When this changes, check script/notices/ghostty-vt-deps.txt: release.sh
# fails if libghostty-vt gains code the notices don't cover.
GHOSTTY_REV := 81598385d608dda4946093d6d6a234d204a2f225
VT_PREFIX   := $(CURDIR)/third_party/ghostty-vt
ZIG         ?= $(shell brew --prefix zig@0.16 2>/dev/null)/bin/zig
# Semver from the git tag: v0.1.0 builds as 0.1.0. Commits after it add build
# metadata (0.1.0+3.abc1234, +dirty for local changes), which sorts after the
# release, unlike a -pre-release suffix. Without a tag: 0.0.0-dev+<commit>,
# plus .dirty when the build includes uncommitted changes.
VERSION     ?= $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null | sed -E 's/^v//; s/-([0-9]+)-g([0-9a-f]+)/+\1.\2/; s/\+(.*)-dirty$$/+\1.dirty/; s/-dirty$$/+dirty/' || true)
ifeq ($(VERSION),)
VERSION     := 0.0.0-dev+$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo .dirty)
endif
# ZIG_TARGET (such as x86_64-linux-gnu.2.28) builds libghostty-vt and hive's C
# parts for that target with Zig, so a Linux release runs on older glibc.
ifneq ($(ZIG_TARGET),)
VT_TARGET   := -Dtarget=$(ZIG_TARGET)
export CC   := $(ZIG) cc -target $(ZIG_TARGET)
endif
LDFLAGS     := -s -w -X github.com/manueltarouca/hive/internal/server.Version=$(VERSION)
export PKG_CONFIG_PATH := $(VT_PREFIX)/share/pkgconfig

.PHONY: all build test vt vt-android clean version

all: build

build: $(VT_PREFIX)/lib/libghostty-vt.a
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/hive ./cmd/hive

version:
	@echo $(VERSION)

test: $(VT_PREFIX)/lib/libghostty-vt.a
	go test ./...

vt: $(VT_PREFIX)/lib/libghostty-vt.a

$(VT_PREFIX)/lib/libghostty-vt.a:
	@if [ ! -d third_party/ghostty/.git ]; then \
	  git init -q third_party/ghostty && \
	  git -C third_party/ghostty remote add origin https://github.com/ghostty-org/ghostty.git; fi
	git -C third_party/ghostty fetch -q --depth 1 origin $(GHOSTTY_REV)
	git -C third_party/ghostty checkout -q FETCH_HEAD
	cd third_party/ghostty && $(ZIG) build -Demit-lib-vt -Demit-xcframework=false -Doptimize=ReleaseFast $(VT_TARGET) --prefix $(VT_PREFIX)

# libghostty-vt for the Android app (apps/android), from the same checkout.
# The NDK comes from ANDROID_NDK_HOME, or the newest under the Android SDK.
VT_ANDROID  := $(CURDIR)/third_party/ghostty-vt-android

vt-android: $(VT_ANDROID)/lib/libghostty-vt.a

$(VT_ANDROID)/lib/libghostty-vt.a: $(VT_PREFIX)/lib/libghostty-vt.a
	@if [ -z "$$ANDROID_NDK_HOME" ] && ! ls -d "$${ANDROID_HOME:-$$HOME/Library/Android/sdk}"/ndk/* >/dev/null 2>&1; then \
	  echo "vt-android needs the Android NDK: set ANDROID_NDK_HOME, or install one under the SDK" >&2; exit 1; fi
	cd third_party/ghostty && $(ZIG) build -Demit-lib-vt -Demit-xcframework=false -Doptimize=ReleaseFast -Dtarget=aarch64-linux-android.29 --prefix $(VT_ANDROID)

clean:
	rm -rf bin
