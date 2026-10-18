#!/bin/sh
# hive's remote listeners on a private tailnet: a Headscale control server
# and tailscaled nodes, all in containers on their own network. Nothing here
# touches the machine's own Tailscale or tailnet.
#
#   alice   a person: owns the server node
#   bob     another person on the tailnet
#   ci      a tagged device (tag:ci), whose key alice made
#   srv     alice's node, running hive with tailnet:// and tailnet-serve://
#   tsn     no tailscaled: hive joins the tailnet itself (--tailscale)
#
# Usage: test/tailnet/run.sh   (builds hive-tailnet:dev first; KEEP=1 leaves it up)
set -eu
root="$(cd "$(dirname "$0")/../.." && pwd)"
img=hive-tailnet:dev
hs=ghcr.io/juanfont/headscale:v0.29.5@sha256:780ed2f9e85719a2211277433f2c8afb45105d0d25031949a6848a485f9e216d
net=hive-tailnet
nodes="hs srv alice bob ci tsn"
fail=0

cleanup() {
  [ "${KEEP:-}" = 1 ] && return
  docker rm -f $nodes >/dev/null 2>&1 || true
  docker network rm $net >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

docker build -q -f "$root/test/tailnet/Dockerfile" -t $img "$root" >/dev/null
docker network create $net >/dev/null

# Headscale: MagicDNS
# under hive.test, everyone may reach everyone, alice owns tag:ci.
cfg="$(mktemp -d /tmp/hive-tailnet.XXXX)"
cat > "$cfg/config.yaml" <<EOF
server_url: http://hs:8080
listen_addr: 0.0.0.0:8080
metrics_listen_addr: 127.0.0.1:9090
grpc_listen_addr: 127.0.0.1:50443
noise:
  private_key_path: /var/lib/headscale/noise_private.key
prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48
derp:
  server:
    enabled: false
  urls: []
  paths: [/etc/headscale/derp.yaml]
  auto_update_enabled: false
disable_check_updates: true
database:
  type: sqlite
  sqlite:
    path: /var/lib/headscale/db.sqlite
dns:
  magic_dns: true
  base_domain: hive.test
  override_local_dns: false
  nameservers:
    global: []
policy:
  mode: file
  path: /etc/headscale/policy.json
log:
  level: warn
unix_socket: /var/run/headscale/headscale.sock
unix_socket_permission: "0770"
logtail:
  enabled: false
EOF
# Headscale needs a DERP map; this relay doesn't exist. The nodes reach
# each other directly, and never Tailscale's public relays.
cat > "$cfg/derp.yaml" <<'EOF'
regions:
  900:
    regionid: 900
    regioncode: none
    regionname: Unreachable
    nodes:
      - name: 900a
        regionid: 900
        hostname: derp.invalid
EOF
cat > "$cfg/policy.json" <<'EOF'
{
  "tagOwners": {"tag:ci": ["alice@"]},
  "acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
}
EOF
# Copied in, not mounted: the container engine may run in a VM that
# doesn't share this directory.
docker create --name hs --network $net $hs serve >/dev/null
docker cp "$cfg/config.yaml" hs:/etc/headscale/config.yaml
docker cp "$cfg/policy.json" hs:/etc/headscale/policy.json
docker cp "$cfg/derp.yaml" hs:/etc/headscale/derp.yaml
docker start hs >/dev/null
for i in $(seq 1 50); do docker exec hs headscale users list >/dev/null 2>&1 && break; sleep 0.2; done
docker exec hs headscale users create alice >/dev/null
docker exec hs headscale users create bob >/dev/null
uid() { docker exec hs headscale users list -o json | jq -r --arg n "$1" '.[] | select(.name==$n) | .id'; }
alice_key=$(docker exec hs headscale preauthkeys create --user "$(uid alice)" --reusable --expiration 1h -o json | jq -r .key)
bob_key=$(docker exec hs headscale preauthkeys create --user "$(uid bob)" --reusable --expiration 1h -o json | jq -r .key)
ci_key=$(docker exec hs headscale preauthkeys create --user "$(uid alice)" --reusable --expiration 1h --tags tag:ci -o json | jq -r .key)

# A node: tailscaled on its own TUN device, joined to Headscale.
node() {
  docker run -d --name "$1" --hostname "$1" --network $net --cap-add NET_ADMIN --device /dev/net/tun $img \
    sh -c "tailscaled --state=/var/lib/tailscale/tailscaled.state >/var/log/tailscaled.log 2>&1 & \
           until tailscale status >/dev/null 2>&1 || tailscale status 2>&1 | grep -q Logged; do sleep 0.2; done; \
           tailscale up --login-server http://hs:8080 --authkey '$2' --hostname '$1' --accept-dns=false && sleep infinity" >/dev/null
}
node srv "$alice_key"
node alice "$alice_key"
node bob "$bob_key"
node ci "$ci_key"
for n in srv alice bob ci; do
  for i in $(seq 1 100); do docker exec $n tailscale ip -4 >/dev/null 2>&1 && break; sleep 0.3; done
done
srv_ip=$(docker exec srv tailscale ip -4)
# warm A B: wait until WireGuard carries traffic between them (a TSMP ping
# goes through it; a disco ping does not). This tailnet has no relay.
warm() { for i in $(seq 1 60); do docker exec "$1" tailscale ping --tsmp -c 1 --timeout 2s "$2" >/dev/null 2>&1 && return; sleep 0.5; done; echo "no path $1 -> $2"; }
for n in alice bob ci; do warm $n srv; done

# hive on srv, as alice's node: the tailnet address and the Serve socket.
hive_up() {
  docker exec srv sh -c 'pkill -x hive 2>/dev/null; sleep 0.5; mkdir -p /run/hive'
  docker exec -d srv sh -c "hive server run --socket /run/hive/local.sock --state-dir /tmp/hive-state \
    --extra-listen tailnet://:4483 --extra-listen tailnet-serve:///run/hive/serve.sock $* >/tmp/hive.log 2>&1"
  for i in $(seq 1 50); do docker exec srv test -S /run/hive/serve.sock && break; sleep 0.2; done
}
hive_up
docker exec srv tailscale serve --bg --http=80 unix:/run/hive/serve.sock >/dev/null

# call FROM URL [HOST]: whoami, printing "STATUS PRINCIPAL".
call() {
  docker exec "$1" rm -f /tmp/out
  docker exec "$1" curl -s --max-time 10 -o /tmp/out -w '%{http_code}' ${3:+-H "Host: $3"} \
    -H 'content-type: application/json' -d '{"method":"whoami"}' "$2/v1/call" > /tmp/hive-tailnet-code 2>/dev/null || true
  code=$(cat /tmp/hive-tailnet-code)
  p=$(docker exec "$1" sh -c 'jq -r ".result.principal // .error.code" /tmp/out 2>/dev/null' || true)
  echo "$code $p"
}
check() { # WHAT GOT WANT
  if [ "$2" = "$3" ]; then echo "ok    $1: $2"; else echo "FAIL  $1: got '$2', want '$3'"; fail=$((fail+1)); fi
}

echo "== tailnet:// (WhoIs)"
check "alice, the owner"        "$(call alice http://$srv_ip:4483)" "200 tailscale:alice"
check "bob"                     "$(call bob http://$srv_ip:4483)"   "403 forbidden"
check "ci, tagged by alice"     "$(call ci http://$srv_ip:4483)"    "403 forbidden"
check "alice, rebound Host"     "$(call alice http://$srv_ip:4483 evil.example)" "403 forbidden"
check "alice, by MagicDNS name" "$(call alice http://$srv_ip:4483 srv.hive.test)" "200 tailscale:alice"
# Another account on the server's own machine, through its tailnet address:
# WhoIs names the node itself (its owner). Refused; local callers use the
# socket.
self_code=$(docker exec -u alice srv curl -s --max-time 10 -o /dev/null -w '%{http_code}' \
  -H 'content-type: application/json' -d '{"method":"whoami"}' http://$srv_ip:4483/v1/call || true)
check "a local account via the tailnet address" "$self_code" "403"

echo "== the CLI over the tailnet"
# hive_cli NODE ARGS...: the hive CLI on a node, its own settings dir.
hive_cli() { n=$1; shift; docker exec -e HIVE_CONFIG=/tmp/cli/config -e HIVE_SERVER=/tmp/cli/none.sock "$n" hive "$@" 2>&1; }
check "alice: hive -S URL whoami" "$(hive_cli alice -S http://$srv_ip:4483 whoami | cut -d' ' -f1-3)" "tailscale:alice via tailnet"
check "alice: a bare host (https:443, https:4483, then http:4483)" "$(hive_cli alice -S $srv_ip whoami | cut -d' ' -f1-3)" "tailscale:alice via tailnet"
hive_cli alice hosts add srv http://$srv_ip:4483 >/dev/null
hive_cli alice -S srv new from-alice -- cat >/dev/null
check "alice: a saved host, new + ls" "$(hive_cli alice -S srv ls | grep -c from-alice)" "1"
check "bob: refused, and told why" "$(hive_cli bob -S http://$srv_ip:4483 ls | grep -o 'tailscale:bob may not use this server')" "tailscale:bob may not use this server"
bob_exit=$(docker exec -e HIVE_CONFIG=/tmp/cli/config -e HIVE_SERVER=/tmp/cli/none.sock bob hive -S http://$srv_ip:4483 ls >/dev/null 2>&1 && echo 0 || echo $?)
check "bob: exit status 5 (refused)" "$bob_exit" "5"
check "nothing started locally" "$(docker exec alice sh -c 'test -e /tmp/cli/none.sock && echo started || echo no')" "no"

echo "== tailnet-serve:// (Serve's header)"
check "alice, the owner" "$(call alice http://$srv_ip srv.hive.test)" "200 tailscale:alice"
check "bob"              "$(call bob http://$srv_ip srv.hive.test)"   "403 forbidden"
check "ci (Serve doesn't name tagged devices)" "$(call ci http://$srv_ip srv.hive.test)" "403 forbidden"
# Serve matches requests by the node's name; these containers don't use
# MagicDNS, so alice learns it from /etc/hosts.
docker exec alice sh -c "echo '$srv_ip srv.hive.test' >> /etc/hosts"
check "alice: hive -S http://srv.hive.test whoami, through Serve" "$(hive_cli alice -S http://srv.hive.test whoami | cut -d, -f1)" "tailscale:alice via tailnet-serve"

echo "== --tailnet-allow-identified"
hive_up --tailnet-allow-identified
check "bob"  "$(call bob http://$srv_ip:4483)" "200 tailscale:bob"
check "ci"   "$(call ci http://$srv_ip:4483)"  "200 tailscale:tag:ci"
check "bob through Serve" "$(call bob http://$srv_ip srv.hive.test)" "200 tailscale:bob"

echo "== --tailscale (built in, no tailscaled)"
docker run -d --name tsn --hostname tsn --network $net $img sleep infinity >/dev/null
docker exec tsn sh -c "printf %s '$alice_key' > /tmp/key && chmod 600 /tmp/key"
docker exec -d tsn sh -c "hive server run --socket /tmp/local.sock --state-dir /tmp/hive-state --tailscale \
  --tailscale-hostname hive-ts --tailscale-control-url http://hs:8080 --tailscale-auth-key-file /tmp/key >/tmp/hive.log 2>&1"
for i in $(seq 1 100); do docker exec alice tailscale ip -4 hive-ts >/dev/null 2>&1 && break; sleep 0.3; done
ts_ip=$(docker exec alice tailscale ip -4 hive-ts 2>/dev/null || echo none)
for n in alice bob ci; do warm $n hive-ts; done
for i in $(seq 1 50); do [ "$(call alice http://$ts_ip:4483 | cut -d' ' -f1)" = 200 ] && break; sleep 0.3; done
check "alice, the owner" "$(call alice http://$ts_ip:4483)" "200 tailscale:alice"
check "bob"              "$(call bob http://$ts_ip:4483)"   "403 forbidden"
check "ci"               "$(call ci http://$ts_ip:4483)"    "403 forbidden"
st=$(docker exec tsn hive -S /tmp/local.sock server status | jq -r '.tailscale.state')
check "server status shows the node" "$st" "Running"
check "alice: hive -S URL whoami, built in" "$(hive_cli alice -S http://$ts_ip:4483 whoami | cut -d' ' -f1-3)" "tailscale:alice via tailscale"

echo "== the key stays out of the panes"
docker exec tsn hive -S /tmp/local.sock new keycheck -- sh -c 'env > /tmp/pane-env; sleep 30' >/dev/null
sleep 1
check "TS_AUTHKEY in a pane" "$(docker exec tsn sh -c 'grep -c TS_AUTHKEY /tmp/pane-env || true')" "0"

case "$cfg" in /tmp/hive-tailnet.*) rm -rf "$cfg";; esac
if [ $fail -gt 0 ]; then
  echo "$fail failed"; docker exec srv tail -20 /tmp/hive.log; exit 1
fi
echo "all passed"
