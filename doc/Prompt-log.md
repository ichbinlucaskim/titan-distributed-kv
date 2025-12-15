# 🛠️ Titan-KV (Distributed Key-Value Store) Project Plan

This plan is structured into four major phases, starting from fundamental concepts and progressing to a fully functional, replicated, and sharded distributed system.

### Phase 1: Core Storage Engine (Single Node)

The goal of this phase is to build the foundational storage layer, inspired by **Bitcask**, to demonstrate an understanding of database internals (Storage Engine).

| Step | Detailed English Prompt | Core Concept |
| :--- | :--- | :--- |
| **1.1** | **Implement the core Key-Value storage mechanism.** This involves building a simple in-memory **hash map** to store key-value pairs and using an append-only **log file** (Data File) on disk for persistence. The key should map to the record's location (file ID, offset, size) in the log file, not the value itself. | **Bitcask Data Structure & Persistence** |
| **1.2** | **Implement a Garbage Collection (GC) mechanism.** Write a routine to periodically merge the Data Files, discarding keys that have been overwritten or deleted, to reclaim disk space. | **Log-Structured Storage & Compaction** |
| **1.3** | **Add an API for PUT, GET, and DELETE operations.** Ensure all operations are durable (written to the log file) before acknowledging success. | **Basic Data Operations** |

---

### Phase 2: Communication and Protocol Layer (gRPC)

In this phase, we introduce the network communication layer using **Go gRPC**.

| Step | Detailed English Prompt | Core Concept |
| :--- | :--- | :--- |
| **2.1** | **Define the protobuf schema** for the KV store operations (PUT, GET, DELETE) and node-to-node communication (e.g., Replication request). | **Protocol Buffers & Schema Definition** |
| **2.2** | **Implement the gRPC Server and Client** interfaces in Go. The server should expose the KV APIs defined in Step 1.3, making the single-node store accessible over the network. | **Go gRPC Implementation** |
| **2.3** | **Implement a simple Command Line Interface (CLI) client** to interact with the gRPC server to test network connectivity and operations. | **Client-Server Architecture** |

---

### Phase 3: Distributed Consensus and Replication (RAFT/Paxos)

This is the most critical phase for demonstrating CS depth. We will implement **Replication** using a consensus algorithm.

| Step | Detailed English Prompt | Core Concept |
| :--- | :--- | :--- |
| **3.1** | **Integrate the RAFT consensus algorithm** (e.g., using an existing Go library like `hashicorp/raft` or a simplified custom implementation). The system must now run in a cluster of at least three nodes. | **Consensus Algorithm (RAFT)** |
| **3.2** | **Implement a Finite State Machine (FSM)** that applies all key-value changes (**PUTs/DELETEs**) to the underlying Storage Engine (Phase 1). All client write requests must be first committed via RAFT before being applied to the FSM. | **State Machine Replication** |
| **3.3** | **Handle Leader Election and Log Replication.** Ensure data is replicated across the cluster and that the system remains available during a leader failure. | **Fault Tolerance & High Availability** |



---

### Phase 4: Data Distribution and Scalability (Sharding)

The final phase addresses scalability by implementing **Sharding** (partitioning) across the cluster, inspired by **Dynamo architecture**.

| Step | Detailed English Prompt | Core Concept |
| :--- | :--- | :--- |
| **4.1** | **Implement a consistent hashing ring.** Use a consistent hashing library or custom logic to map keys to specific nodes (shards) in the cluster. This is the **Ring/Membership Service**. | **Consistent Hashing & Partitioning** |
| **4.2** | **Implement a Coordinator/Proxy node logic.** Any node receiving a client request must determine the correct destination shard using the hashing ring. If the key belongs to another node, the request must be **forwarded** to the correct node/leader. | **Decentralized Request Routing** |
| **4.3** | **Implement basic Sharding logic for GET/PUT requests.** When a node receives a PUT, it calculates the target shard. If it's the target, it commits via RAFT (Phase 3). If not, it forwards the request. | **Sharding Implementation** |
| **4.4** | **(Optional but Highly Recommended) Implement basic Read Repair or hinted handoff.** When a read request (GET) returns conflicting data or a node is unavailable, implement a simple mechanism to repair the inconsistency or temporarily store the write for later delivery. | **Dynamo-style Conflict Resolution** |

---

I have completed the Titan-KV distributed key-value store with RAFT consensus and Sharding. Now I need to implement a robust **Integration Test Suite** to verify fault tolerance.

Please act as a Senior Go Engineer and write a comprehensive integration test file (`tests/integration_test.go`).

### 1. Requirements
- Use the `github.com/stretchr/testify/require` library for assertions.
- Do NOT rely on external shell scripts. The test must spin up the cluster **programmatically** within the Go test process (using goroutines).
- Use temporary directories (`t.TempDir()`) for RAFT logs and Bitcask storage to ensure clean state for every test.

### 2. Implementation Details
Please implement a helper struct `TestCluster` with methods:
- `NewTestCluster(t *testing.T, numNodes int) *TestCluster`: Starts N nodes with random free ports.
- `cluster.StopNode(nodeIndex int)`: Simulates a crash (stops gRPC server and Raft node).
- `cluster.StartNode(nodeIndex int)`: Restarts a node (simulates recovery).
- `cluster.GetLeader() *Server`: Returns the current leader node.

### 3. Test Scenarios
Write the following test functions:
1. **Test_BasicWriteRead**:
   - Start a 3-node cluster.
   - Write Key="alpha", Value="beta" to the Leader.
   - Read Key="alpha" from a Follower node (verify replication).

2. **Test_LeaderFailure_DataPersistence**:
   - Start 3 nodes.
   - Write Key="resilience", Value="test".
   - **Identify and Stop the Leader node.**
   - Wait for a new Leader to be elected among the remaining 2 nodes.
   - Write a new Key="after_crash", Value="success" to the NEW Leader.
   - **Restart the old Leader.**
   - Verify that the old Leader eventually catches up and contains both "resilience" and "after_crash".

### 4. Constraints
- Handle dynamic port allocation to avoid "Address already in use" errors.
- Include appropriate `time.Sleep` or polling mechanisms to wait for Leader election stability.


---

Great, the integration tests are passing! Now I want to package Titan-KV for easy deployment and demonstration using Docker.

Please provide the necessary Docker configuration files.

### 1. Dockerfile
Create a `Dockerfile` for the Titan-KV server.
- Use a **multi-stage build**: Build in `golang:1.21-alpine`, run in a minimal `alpine` or `distroless` image.
- Expose the necessary gRPC and RAFT ports.
- Define a volume mount point for persistent data.

### 2. docker-compose.yml
Create a `docker-compose.yml` to spin up a **3-node cluster**.
- Service names: `node1`, `node2`, `node3`.
- Networking: Ensure they can talk to each other using their service names (DNS).
- **Startup Command**:
    - Node 1 starts as the bootstrap leader.
    - Node 2 and Node 3 should start and automatically join Node 1.
    - You might need a simple shell script (`entrypoint.sh`) to handle the "join" logic (e.g., `retry until join success`).

### 3. Usage Instructions
Briefly explain how to run the cluster and how to use the CLI tool from the host machine to connect to the Dockerized cluster.

---
