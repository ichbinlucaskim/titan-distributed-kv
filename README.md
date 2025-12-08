# Titan-KV: Distributed Key-Value Store

A distributed key-value store implementation built in Go, progressing from a single-node Bitcask-style storage engine to a fully replicated and sharded distributed system.

## Project Status

### ✅ Phase 1: Core Storage Engine (Single Node) - COMPLETE

Phase 1 implements a Bitcask-inspired storage engine with the following features:

- **Append-only log file**: All writes are appended to disk for durability
- **In-memory hash map (keyDir)**: Maps keys to record locations (file ID, offset, size) in log files
- **Garbage Collection**: Compaction mechanism to merge data files and reclaim disk space
- **Durable operations**: PUT, GET, and DELETE operations with durability guarantees (fsync)

#### Architecture

- `storage/bitcask.go`: Core storage engine implementation
- `storage/compaction.go`: Garbage collection and log compaction
- `storage/api.go`: High-level API interface
- `cmd/kv/main.go`: Example program demonstrating the storage engine

#### Key Features

1. **Bitcask Data Structure & Persistence** (Step 1.1)
   - In-memory hash map (`keyDir`) storing key → record location mappings
   - Append-only log files (`data-{id}.log`) for persistence
   - Automatic file rotation when files exceed size limits

2. **Log-Structured Storage & Compaction** (Step 1.2)
   - Periodic compaction merges multiple data files into one
   - Removes deleted/overwritten keys to reclaim disk space
   - Maintains data integrity during compaction

3. **Basic Data Operations** (Step 1.3)
   - `PUT`: Store key-value pairs with durability guarantee
   - `GET`: Retrieve values by key
   - `DELETE`: Remove keys (uses tombstone markers)

#### Usage

```bash
# Run the example program
go run cmd/kv/main.go

# Or specify a custom data directory
go run cmd/kv/main.go -data-dir ./my-data
```

The example program demonstrates:
- PUT operations (including updates)
- GET operations
- DELETE operations
- Automatic background compaction every 5 minutes

### ✅ Phase 2: Communication and Protocol Layer (gRPC) - COMPLETE

Phase 2 adds network communication capabilities using gRPC, making the storage engine accessible over the network.

#### Architecture

- `proto/kvstore.proto`: Protocol Buffer schema definition
- `proto/kvstore/kvstore.pb.go`: Generated gRPC code
- `server/server.go`: gRPC server implementation
- `client/client.go`: gRPC client library
- `cmd/server/main.go`: gRPC server executable
- `cmd/cli/main.go`: CLI client tool

#### Key Features

1. **Protocol Buffers & Schema Definition** (Step 2.1)
   - Defined protobuf schema for KV operations (PUT, GET, DELETE)
   - Defined replication service for future Phase 3 implementation
   - Generated Go code from protobuf definitions

2. **Go gRPC Implementation** (Step 2.2)
   - gRPC server exposing KV APIs from Phase 1
   - gRPC client library with timeout support
   - Graceful shutdown handling
   - Error handling and validation

3. **Client-Server Architecture** (Step 2.3)
   - Interactive CLI client for testing
   - Network connectivity testing
   - Command-line interface for PUT, GET, DELETE operations

#### Usage

**Start the gRPC server:**
```bash
# Run the server (default: :50051)
go run cmd/server/main.go

# Or specify custom address and data directory
go run cmd/server/main.go -address :8080 -data-dir ./server-data
```

**Use the CLI client:**
```bash
# Connect to default server (localhost:50051)
go run cmd/cli/main.go

# Connect to custom server address
go run cmd/cli/main.go -address localhost:8080
```

**CLI Commands:**
- `PUT <key> <value>` - Store a key-value pair
- `GET <key>` - Retrieve a value by key
- `DELETE <key>` - Delete a key-value pair
- `QUIT` - Exit the client

**Example Session:**
```
titan-kv> PUT user:1 Alice
OK: Stored key 'user:1'
titan-kv> GET user:1
Value: Alice
titan-kv> PUT user:1 Alice Updated
OK: Stored key 'user:1'
titan-kv> GET user:1
Value: Alice Updated
titan-kv> DELETE user:1
OK: Deleted key 'user:1'
titan-kv> QUIT
Goodbye!
```

### ✅ Phase 3: Distributed Consensus and Replication (RAFT) - COMPLETE

Phase 3 adds distributed consensus and replication using the RAFT algorithm, enabling fault tolerance and high availability.

#### Architecture

- `raft/fsm.go`: Finite State Machine implementing `raft.FSM` interface
- `raft/node.go`: RAFT node wrapper and cluster management
- `raft/join.go`: Cluster membership management
- `server/raft_server.go`: gRPC server with RAFT integration
- `scripts/start-cluster.sh`: Script to start a 3-node cluster
- `scripts/stop-cluster.sh`: Script to stop all cluster nodes

#### Key Features

1. **Consensus Algorithm (RAFT)** (Step 3.1)
   - Integrated `hashicorp/raft` library
   - RAFT node configuration and initialization
   - Cluster bootstrap and node joining
   - Persistent log store, stable store, and snapshot store

2. **State Machine Replication** (Step 3.2)
   - FSM that applies PUT/DELETE operations to storage engine
   - All write requests committed via RAFT before applying to FSM
   - Read operations served directly from local storage (no RAFT overhead)
   - Snapshot support for efficient state recovery

3. **Fault Tolerance & High Availability** (Step 3.3)
   - Automatic leader election
   - Log replication across cluster
   - System remains available during leader failure
   - Read requests can be served by any node
   - Write requests automatically redirected to leader

#### Usage

**Start a single node (for testing):**
```bash
# Start a bootstrap node
go run cmd/server/main.go \
  -node-id node1 \
  -grpc-addr :50051 \
  -raft-addr :12000 \
  -data-dir ./data/node1 \
  -bootstrap
```

**Start a 3-node cluster:**
```bash
# Start all 3 nodes
./scripts/start-cluster.sh

# Stop all nodes
./scripts/stop-cluster.sh
```

**Manual cluster setup:**
```bash
# Terminal 1: Start Node 1 (Bootstrap)
go run cmd/server/main.go -node-id node1 -grpc-addr :50051 -raft-addr :12000 -data-dir ./data/node1 -bootstrap

# Terminal 2: Start Node 2
go run cmd/server/main.go -node-id node2 -grpc-addr :50052 -raft-addr :12001 -data-dir ./data/node2

# Terminal 3: Start Node 3
go run cmd/server/main.go -node-id node3 -grpc-addr :50053 -raft-addr :12002 -data-dir ./data/node3
```

**Test the cluster:**
```bash
# Connect to any node (reads work on all nodes)
go run cmd/cli/main.go -address localhost:50051

# Writes will be redirected to leader if not connected to leader
titan-kv> PUT mykey "Hello from cluster"
OK: Stored key 'mykey'
titan-kv> GET mykey
Value: Hello from cluster
```

#### Cluster Behavior

- **Leader Election**: RAFT automatically elects a leader when cluster starts
- **Write Operations**: PUT/DELETE operations must go through the leader
- **Read Operations**: GET operations can be served by any node
- **Fault Tolerance**: Cluster can tolerate (N-1)/2 node failures (for 3 nodes, 1 failure is tolerated)
- **Data Replication**: All writes are replicated to all nodes via RAFT log
- **Automatic Recovery**: If leader fails, a new leader is automatically elected

### ✅ Phase 4: Data Distribution and Scalability (Sharding) - COMPLETE

Phase 4 implements sharding (partitioning) across the cluster using consistent hashing, enabling horizontal scalability and distributed data storage.

#### Architecture

- `sharding/ring.go`: Consistent hashing ring implementation
- `sharding/membership.go`: Cluster membership service and node tracking
- `server/sharded_server.go`: Sharded gRPC server with request routing
- `scripts/start-sharded-cluster.sh`: Script to start a sharded cluster

#### Key Features

1. **Consistent Hashing & Partitioning** (Step 4.1)
   - Consistent hashing ring with virtual nodes (150 replicas per node)
   - SHA-256 hash function for key-to-node mapping
   - Automatic key distribution across shards
   - Support for multiple replicas per key

2. **Decentralized Request Routing** (Step 4.2)
   - Any node can receive client requests
   - Automatic determination of target shard using hash ring
   - Request forwarding to correct shard if needed
   - Transparent routing - clients don't need to know shard locations

3. **Sharding Implementation** (Step 4.3)
   - GET requests: Routed to correct shard, served from local storage
   - PUT/DELETE requests: Routed to correct shard, committed via RAFT
   - Automatic forwarding when request arrives at wrong shard
   - Each shard maintains its own RAFT cluster for replication

4. **Dynamo-style Conflict Resolution** (Step 4.4)
   - Read repair: Automatically repairs missing keys on replicas
   - Value comparison: Detects and logs value conflicts
   - Background repair: Runs asynchronously after reads
   - Configurable replication factor for read repair

#### Usage

**Start a sharded cluster:**
```bash
# Start all 3 nodes with sharding enabled
./scripts/start-sharded-cluster.sh

# Stop all nodes
./scripts/stop-cluster.sh
```

**Manual sharded cluster setup:**
```bash
# Terminal 1: Start Node 1 (Bootstrap)
go run cmd/server/main.go \
  -node-id node1 \
  -grpc-addr localhost:50051 \
  -raft-addr localhost:12000 \
  -data-dir ./data/node1 \
  -bootstrap \
  -peers "node2|localhost:50052|localhost:12001,node3|localhost:50053|localhost:12002"

# Terminal 2: Start Node 2
go run cmd/server/main.go \
  -node-id node2 \
  -grpc-addr localhost:50052 \
  -raft-addr localhost:12001 \
  -data-dir ./data/node2 \
  -peers "node1|localhost:50051|localhost:12000,node3|localhost:50053|localhost:12002"

# Terminal 3: Start Node 3
go run cmd/server/main.go \
  -node-id node3 \
  -grpc-addr localhost:50053 \
  -raft-addr localhost:12002 \
  -data-dir ./data/node3 \
  -peers "node1|localhost:50051|localhost:12000,node2|localhost:50052|localhost:12001"
```

**Test sharding:**
```bash
# Connect to any node
go run cmd/cli/main.go -address localhost:50051

# Keys are automatically sharded across nodes
titan-kv> PUT user:1 "Alice"
OK: Stored key 'user:1'
titan-kv> PUT user:2 "Bob"
OK: Stored key 'user:2'
titan-kv> PUT product:1 "Widget"
OK: Stored key 'product:1'

# Reads work from any node (will forward if needed)
titan-kv> GET user:1
Value: Alice
```

#### How Sharding Works

1. **Key Routing**: When a request arrives, the node computes the hash of the key
2. **Shard Determination**: Hash ring determines which node owns the key
3. **Request Handling**:
   - If local node owns the key: Process directly (write via RAFT, read from storage)
   - If remote node owns the key: Forward request to that node
4. **Read Repair**: After successful reads, background process checks replicas and repairs inconsistencies

#### Benefits

- **Horizontal Scalability**: Add more nodes to increase capacity
- **Load Distribution**: Keys distributed evenly across nodes
- **Fault Tolerance**: Each shard replicated via RAFT
- **Transparent Routing**: Clients don't need to know shard locations
- **Automatic Rebalancing**: Consistent hashing minimizes key movement when nodes join/leave

## Project Structure

```
titan-distributed-kv/
├── storage/          # Core storage engine (Phase 1)
│   ├── bitcask.go   # Bitcask implementation
│   ├── compaction.go # Garbage collection
│   └── api.go       # High-level API
├── proto/            # Protocol Buffers (Phase 2)
│   ├── kvstore.proto # Protobuf schema
│   └── kvstore/      # Generated code
│       └── kvstore.pb.go
├── server/           # gRPC server (Phase 2)
│   └── server.go
├── client/           # gRPC client library (Phase 2)
│   └── client.go
├── raft/            # RAFT consensus (Phase 3)
│   ├── fsm.go      # Finite State Machine
│   ├── node.go     # RAFT node wrapper
│   └── join.go     # Cluster membership
├── sharding/        # Sharding and consistent hashing (Phase 4)
│   ├── ring.go     # Consistent hashing ring
│   └── membership.go # Cluster membership service
├── cmd/
│   ├── kv/          # Example program (Phase 1)
│   │   └── main.go
│   ├── server/      # gRPC server executable (Phase 2/3)
│   │   └── main.go
│   └── cli/         # CLI client (Phase 2)
│       └── main.go
├── scripts/         # Cluster management scripts
│   ├── start-cluster.sh        # Start non-sharded cluster (Phase 3)
│   ├── start-sharded-cluster.sh # Start sharded cluster (Phase 4)
│   └── stop-cluster.sh
├── doc/
│   └── Prompt-log.md # Project plan and phases
├── Makefile          # Build automation
└── README.md
```

## Requirements

- Go 1.21 or later
- Protocol Buffer compiler (`protoc`) - optional, generated code is included
- gRPC Go plugins - optional, generated code is included
- `hashicorp/raft` and `hashicorp/raft-boltdb` - included in go.mod

## Building

**Install dependencies:**
```bash
go mod download
go mod tidy
```

**Build binaries:**
```bash
# Build everything
make build

# Or build individually
make server  # Build server only
make cli     # Build CLI client only
```

**Regenerate protobuf code (if needed):**
```bash
make proto
```

The binaries will be created in the `bin/` directory:
- `bin/server` - gRPC server
- `bin/cli` - CLI client

## License

MIT