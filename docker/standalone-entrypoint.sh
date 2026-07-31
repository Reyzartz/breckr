#!/bin/sh
# Starts the bundled browser, then hands off to the server.
#
# Only the :standalone image runs this. The slim image talks to a browser in
# another container and needs no entrypoint at all.
set -e

# Alpine installs `chromium`; some releases also ship a `chromium-browser`
# symlink. Look for both rather than pinning the name a package rename could
# change under us.
CHROME="$(command -v chromium || command -v chromium-browser || true)"
if [ -z "$CHROME" ]; then
  echo "no chromium in this image -- is this the :standalone tag?" >&2
  exit 1
fi

# --remote-debugging-address=127.0.0.1 is the load-bearing flag here. An open
# CDP port is remote code execution: anyone who can reach it can drive the
# browser, read files through it and pivot into the host. Bound to the
# container's own loopback it is reachable by the server in this container and
# by nothing else, and port 9222 is never published.
#
# --no-sandbox because Chromium's sandbox wants root+SUID or user namespaces,
# and a default container grants neither. The container is the boundary
# instead, which is only true because of the line above.
#
# --disable-dev-shm-usage because `docker run` gives /dev/shm 64MB and Chromium
# crashes on it under real page loads. --shm-size=1g is still worth passing;
# this only keeps the default from being fatal.
"$CHROME" \
  --headless=new \
  --remote-debugging-port=9222 \
  --remote-debugging-address=127.0.0.1 \
  --no-sandbox \
  --disable-gpu \
  --disable-dev-shm-usage \
  --no-first-run \
  --no-default-browser-check \
  --disable-extensions \
  $CHROMIUM_FLAGS &

# Wait for CDP before starting the server, so the first cron tick does not fire
# into a browser that is still coming up.
attempt=0
until wget -q -O /dev/null http://127.0.0.1:9222/json/version 2>/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -gt 60 ]; then
    echo "chromium did not answer on 127.0.0.1:9222 after 30s" >&2
    exit 1
  fi
  sleep 0.5
done

# exec so the server inherits this process rather than being a child of it:
# that is what puts `docker stop`'s SIGTERM in front of the graceful shutdown
# in main.go rather than in front of a shell that would ignore it. tini, as
# PID 1, reaps the Chromium left behind.
exec "$@"
