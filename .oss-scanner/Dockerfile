# Builds hive and everything its tests need, so they run without a network:
# Go 1.27, Zig 0.16 (Ghostty's terminal library), the Go modules, and a
# warm build cache. Build from the repository's root:
#   docker build -f .oss-scanner/Dockerfile .
FROM golang:1.27-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
      git make pkg-config xz-utils curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Zig 0.16 exactly: Ghostty's build refuses other versions. Checked by hash.
RUN curl -fsSL -o /tmp/zig.tar.xz https://ziglang.org/download/0.16.0/zig-x86_64-linux-0.16.0.tar.xz \
    && echo "70e49664a74374b48b51e6f3fdfbf437f6395d42509050588bd49abe52ba3d00  /tmp/zig.tar.xz" | sha256sum -c - \
    && mkdir -p /opt/zig && tar -xJf /tmp/zig.tar.xz -C /opt/zig --strip-components=1 && rm /tmp/zig.tar.xz
# A fixed target, as the Linux release uses: not the build machine's CPU.
ENV ZIG=/opt/zig/zig PATH=/opt/zig:$PATH ZIG_TARGET=x86_64-linux-gnu.2.28

WORKDIR /src
COPY . .

# libghostty-vt from the pinned Ghostty commit, then hive, then the test
# binaries, so `make test` needs nothing from the network.
RUN make vt && make build && go mod download && go test -count=1 -run XXX_build_only ./...

# Tests start /bin/sh and a throwaway server in a temporary folder.
CMD ["make", "test"]
