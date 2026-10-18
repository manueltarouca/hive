# hive and tailscaled in one Linux image, for the private test tailnet
# (test/tailnet/run.sh). Built from source: Go and Zig checked against
# their official checksums, Tailscale from its signed apt repository.
FROM docker.io/library/ubuntu:24.04@sha256:18b33ff3829707f31ada654a963b9ee35f2e6f0241b964bd17a22a9b330cb63d AS build
ARG GO_VERSION=1.27.2
ARG ZIG_VERSION=0.16.0
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      build-essential git pkg-config curl ca-certificates xz-utils jq && rm -rf /var/lib/apt/lists/*
RUN set -e; arch=$(dpkg --print-architecture); \
    f=go${GO_VERSION}.linux-${arch}.tar.gz; \
    sum=$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" | jq -r --arg f "$f" '.[].files[] | select(.filename==$f) | .sha256'); \
    curl -fsSLO "https://go.dev/dl/$f" && echo "$sum  $f" | sha256sum -c - && tar -C /usr/local -xzf "$f" && rm "$f"; \
    za=$(uname -m)-linux; zj=$(curl -fsSL https://ziglang.org/download/index.json | jq -r --arg v "$ZIG_VERSION" --arg a "$za" '.[$v][$a]'); \
    zu=$(echo "$zj" | jq -r .tarball); zs=$(echo "$zj" | jq -r .shasum); zf=$(basename "$zu"); \
    curl -fsSLO "$zu" && echo "$zs  $zf" | sha256sum -c - && mkdir -p /opt/zig && tar -C /opt/zig --strip-components=1 -xJf "$zf" && rm "$zf"
ENV PATH=/usr/local/go/bin:$PATH
WORKDIR /src
# libghostty first, so code changes don't rebuild it.
COPY Makefile ./
COPY script/notices ./script/notices
RUN make vt ZIG=/opt/zig/zig
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build ZIG=/opt/zig/zig

FROM docker.io/library/ubuntu:24.04@sha256:18b33ff3829707f31ada654a963b9ee35f2e6f0241b964bd17a22a9b330cb63d
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      curl ca-certificates gnupg jq iproute2 procps && \
    curl -fsSL https://pkgs.tailscale.com/stable/ubuntu/noble.noarmor.gpg -o /usr/share/keyrings/tailscale-archive-keyring.gpg && \
    curl -fsSL https://pkgs.tailscale.com/stable/ubuntu/noble.tailscale-keyring.list -o /etc/apt/sources.list.d/tailscale.list && \
    apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends tailscale && \
    rm -rf /var/lib/apt/lists/* && useradd -m alice
COPY --from=build /src/bin/hive /usr/local/bin/hive
