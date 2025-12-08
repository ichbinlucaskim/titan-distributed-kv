#!/bin/sh
set -e

NODE_ID="${NODE_ID:-node1}"
GRPC_ADDR="${GRPC_ADDR:-:50051}"
RAFT_ADDR="${RAFT_ADDR:-:12000}"
DATA_DIR="${DATA_DIR:-/data}"
BOOTSTRAP="${BOOTSTRAP:-false}"
PEERS="${PEERS:-}"
WAIT_FOR="${WAIT_FOR:-}"

wait_for_host() {
  hostport="$1"
  host="${hostport%%:*}"
  port="${hostport##*:}"
  echo "Waiting for $host:$port to be reachable..."
  for i in $(seq 1 60); do
    if nc -z "$host" "$port" >/dev/null 2>&1; then
      echo "$host:$port is reachable."
      return 0
    fi
    sleep 1
  done
  echo "Timeout waiting for $host:$port"
  return 1
}

if [ -n "$WAIT_FOR" ]; then
  wait_for_host "$WAIT_FOR"
fi

CMD="/app/server \
  -node-id ${NODE_ID} \
  -grpc-addr ${GRPC_ADDR} \
  -raft-addr ${RAFT_ADDR} \
  -data-dir ${DATA_DIR}"

if [ "$BOOTSTRAP" = "true" ]; then
  CMD="$CMD -bootstrap"
fi

if [ -n "$PEERS" ]; then
  CMD="$CMD -peers ${PEERS}"
fi

echo "Starting Titan-KV node: ${CMD}"
exec sh -c "$CMD"

