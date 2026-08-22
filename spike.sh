#!/usr/bin/env bash
# Spike: does podman + krun actually give us a microVM we can exec into?
# Run this on the dedicated Linux host (needs KVM). Nothing here is msb yet.
set -u

IMAGE=docker.io/library/alpine:latest
NAME=msb-spike
VOL=/tmp/msb-spike-data

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; }

echo "== host kernel =="
HOST_KERNEL=$(uname -r)
echo "$HOST_KERNEL"

command -v podman >/dev/null || { fail "podman not installed"; exit 1; }
podman --help 2>/dev/null | grep -q runtime || true
[ -e /dev/kvm ] && pass "/dev/kvm present" || { fail "/dev/kvm missing"; exit 1; }

mkdir -p "$VOL"
podman rm -f "$NAME" >/dev/null 2>&1

echo
echo "== 1. can we boot an OCI image as a microVM? =="
podman run -d --name "$NAME" --runtime krun \
  -v "$VOL:/data" "$IMAGE" sleep 600 >/dev/null 2>&1 \
  && pass "booted with --runtime krun" \
  || { fail "could not boot with --runtime krun"; podman logs "$NAME" 2>&1 | tail -5; exit 1; }

echo
echo "== 2. THE CRITICAL TEST: does exec land in the guest kernel? =="
# crun issue #1098: podman exec into a krun container runs on the HOST kernel.
# If this prints the host kernel, exec-based isolation is a lie.
EXEC_KERNEL=$(podman exec "$NAME" uname -r 2>/dev/null)
echo "host : $HOST_KERNEL"
echo "exec : $EXEC_KERNEL"
if [ -z "$EXEC_KERNEL" ]; then
  fail "exec produced no output"
elif [ "$EXEC_KERNEL" = "$HOST_KERNEL" ]; then
  fail "exec ran on the HOST kernel -- NOT isolated (crun#1098 confirmed)"
else
  pass "exec ran on a different kernel -- genuinely inside the VM"
fi

echo
echo "== 3. does the VM's own main process see a guest kernel? =="
MAIN_KERNEL=$(podman run --rm --runtime krun "$IMAGE" uname -r 2>/dev/null)
echo "main : $MAIN_KERNEL"
[ -n "$MAIN_KERNEL" ] && [ "$MAIN_KERNEL" != "$HOST_KERNEL" ] \
  && pass "main process is in a real microVM" \
  || fail "main process kernel matches host -- not a microVM"

echo
echo "== 4. persistence: does the volume survive teardown? =="
podman exec "$NAME" sh -c 'echo survived > /data/marker' 2>/dev/null
podman rm -f "$NAME" >/dev/null 2>&1
podman run --rm --runtime krun -v "$VOL:/data" "$IMAGE" \
  cat /data/marker 2>/dev/null | grep -q survived \
  && pass "host volume persisted across VM destruction" \
  || fail "volume did not persist"

echo
echo "== 5. egress: can the sandbox reach the network? =="
podman run --rm --runtime krun "$IMAGE" \
  sh -c 'wget -q -O- -T5 https://example.com >/dev/null' 2>/dev/null \
  && pass "outbound network works" \
  || fail "no outbound network"

echo
echo "== 6. can the host reach a port inside the guest? (needed if exec goes over ssh) =="
podman rm -f "$NAME" >/dev/null 2>&1
podman run -d --name "$NAME" --runtime krun -p 18080:8080 "$IMAGE" \
  sh -c 'while true; do echo -e "HTTP/1.1 200 OK\r\n\r\nok" | nc -l -p 8080; done' >/dev/null 2>&1
sleep 3
curl -s --max-time 5 http://127.0.0.1:18080 2>/dev/null | grep -q ok \
  && pass "host->guest port forwarding works" \
  || fail "port forwarding did not work (blocks the ssh-based exec plan)"

podman rm -f "$NAME" >/dev/null 2>&1
rm -rf "$VOL"
echo
echo "Done. Test 2 is the one that decides the architecture."
