GHOSTTY_REV := 81598385d608dda4946093d6d6a234d204a2f225
VT_PREFIX   := $(CURDIR)/third_party/ghostty-vt
ZIG         := $(shell brew --prefix zig@0.16 2>/dev/null)/bin/zig
export PKG_CONFIG_PATH := $(VT_PREFIX)/share/pkgconfig

.PHONY: all build test vt clean

all: build

build: $(VT_PREFIX)/lib/libghostty-vt.a
	go build -o bin/hive ./cmd/hive

test: $(VT_PREFIX)/lib/libghostty-vt.a
	go test ./...

vt: $(VT_PREFIX)/lib/libghostty-vt.a

$(VT_PREFIX)/lib/libghostty-vt.a:
	@if [ ! -d third_party/ghostty/.git ]; then \
	  git init -q third_party/ghostty && \
	  git -C third_party/ghostty remote add origin https://github.com/ghostty-org/ghostty.git; fi
	git -C third_party/ghostty fetch -q --depth 1 origin $(GHOSTTY_REV)
	git -C third_party/ghostty checkout -q FETCH_HEAD
	cd third_party/ghostty && $(ZIG) build -Demit-lib-vt -Demit-xcframework=false -Doptimize=ReleaseFast --prefix $(VT_PREFIX)

clean:
	rm -rf bin
