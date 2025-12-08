package tests

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"titan-kv/client"
	"titan-kv/raft"
	"titan-kv/server"
	"titan-kv/storage"
)

// TestServer represents a single node in the test cluster
type TestServer struct {
	NodeID     string
	GrpcAddr   string
	RaftAddr   string
	DataDir    string
	StorageAPI *storage.API
	RaftNode   *raft.Node
	ctx        context.Context
	cancel     context.CancelFunc
	serverErr  chan error
	wg         sync.WaitGroup
}

// TestCluster manages a cluster of test servers
type TestCluster struct {
	t       *testing.T
	servers []*TestServer
	mu      sync.RWMutex
}

// NewTestCluster creates and starts a new test cluster with N nodes
func NewTestCluster(t *testing.T, numNodes int) *TestCluster {
	require.Greater(t, numNodes, 0, "cluster must have at least 1 node")
	require.LessOrEqual(t, numNodes, 10, "cluster size limited to 10 nodes for testing")

	cluster := &TestCluster{
		t:       t,
		servers: make([]*TestServer, numNodes),
	}

	// Create temporary base directory
	baseDir := t.TempDir()

	// Find free ports for all nodes
	grpcPorts := make([]int, numNodes)
	raftPorts := make([]int, numNodes)
	for i := 0; i < numNodes; i++ {
		grpcPorts[i] = getFreePort(t)
		raftPorts[i] = getFreePort(t)
	}

	// Start bootstrap node first
	nodeID := fmt.Sprintf("node%d", 1)
	dataDir := fmt.Sprintf("%s/node%d", baseDir, 1)
	grpcAddr := fmt.Sprintf("localhost:%d", grpcPorts[0])
	raftAddr := fmt.Sprintf("localhost:%d", raftPorts[0])

	server1 := cluster.createServer(nodeID, grpcAddr, raftAddr, dataDir, true)
	cluster.servers[0] = server1
	cluster.startServer(server1)

	// Wait for bootstrap node to be ready
	time.Sleep(2 * time.Second)

	// Start remaining nodes and add them to the cluster
	for i := 1; i < numNodes; i++ {
		nodeID := fmt.Sprintf("node%d", i+1)
		dataDir := fmt.Sprintf("%s/node%d", baseDir, i+1)
		grpcAddr := fmt.Sprintf("localhost:%d", grpcPorts[i])
		raftAddr := fmt.Sprintf("localhost:%d", raftPorts[i])

		server := cluster.createServer(nodeID, grpcAddr, raftAddr, dataDir, false)
		cluster.servers[i] = server
		cluster.startServer(server)

		// Add peer to the cluster via the leader
		time.Sleep(1 * time.Second)
		leader := cluster.GetLeader()
		require.NotNil(t, leader, "leader should exist before adding peer")

		err := leader.RaftNode.AddPeer(nodeID, raftAddr)
		if err != nil {
			// Peer might already be added or leader changed, continue
			t.Logf("Warning: failed to add peer %s: %v", nodeID, err)
		}
	}

	// Wait for cluster to stabilize
	cluster.waitForLeader(t)
	time.Sleep(2 * time.Second)

	return cluster
}

// createServer creates a new test server (but doesn't start it)
func (tc *TestCluster) createServer(nodeID, grpcAddr, raftAddr, dataDir string, bootstrap bool) *TestServer {
	// Create storage engine
	bc, err := storage.NewBitcask(dataDir)
	require.NoError(tc.t, err, "failed to create Bitcask for %s", nodeID)

	// Create compactor
	compactor := storage.NewCompactor(bc)

	// Create API
	api := storage.NewAPI(bc, compactor)

	// Create RAFT node
	raftConfig := raft.Config{
		NodeID:     nodeID,
		BindAddr:   raftAddr,
		DataDir:    dataDir,
		Bootstrap:  bootstrap,
		StorageAPI: api,
	}

	raftNode, err := raft.NewNode(raftConfig)
	require.NoError(tc.t, err, "failed to create RAFT node for %s", nodeID)

	ctx, cancel := context.WithCancel(context.Background())

	return &TestServer{
		NodeID:     nodeID,
		GrpcAddr:   grpcAddr,
		RaftAddr:   raftAddr,
		DataDir:    dataDir,
		StorageAPI: api,
		RaftNode:   raftNode,
		ctx:        ctx,
		cancel:     cancel,
		serverErr:  make(chan error, 1),
	}
}

// startServer starts the gRPC server for a test server
func (tc *TestCluster) startServer(ts *TestServer) {
	ts.wg.Add(1)
	go func() {
		defer ts.wg.Done()
		err := server.StartRaftServerWithContext(ts.ctx, ts.StorageAPI, ts.RaftNode, ts.GrpcAddr)
		if err != nil && err.Error() != "context canceled" {
			select {
			case ts.serverErr <- err:
			default:
			}
		}
	}()
}

// StopNode stops a node (simulates crash)
func (tc *TestCluster) StopNode(nodeIndex int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	require.GreaterOrEqual(tc.t, nodeIndex, 0, "nodeIndex must be >= 0")
	require.Less(tc.t, nodeIndex, len(tc.servers), "nodeIndex out of range")

	server := tc.servers[nodeIndex]
	require.NotNil(tc.t, server, "server at index %d is nil", nodeIndex)

	// Cancel context to stop gRPC server
	server.cancel()

	// Shutdown RAFT node
	err := server.RaftNode.Close()
	if err != nil {
		tc.t.Logf("Warning: error closing RAFT node %s: %v", server.NodeID, err)
	}

	// Close storage
	err = server.StorageAPI.Close()
	if err != nil {
		tc.t.Logf("Warning: error closing storage for %s: %v", server.NodeID, err)
	}

	// Wait for server goroutine to finish
	server.wg.Wait()

	tc.t.Logf("Stopped node %s (index %d)", server.NodeID, nodeIndex)
}

// StartNode restarts a stopped node
func (tc *TestCluster) StartNode(nodeIndex int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	require.GreaterOrEqual(tc.t, nodeIndex, 0, "nodeIndex must be >= 0")
	require.Less(tc.t, nodeIndex, len(tc.servers), "nodeIndex out of range")

	server := tc.servers[nodeIndex]
	require.NotNil(tc.t, server, "server at index %d is nil", nodeIndex)

	// Recreate storage engine (using same data directory)
	bc, err := storage.NewBitcask(server.DataDir)
	require.NoError(tc.t, err, "failed to recreate Bitcask for %s", server.NodeID)

	compactor := storage.NewCompactor(bc)
	api := storage.NewAPI(bc, compactor)
	server.StorageAPI = api

	// Recreate RAFT node
	raftConfig := raft.Config{
		NodeID:     server.NodeID,
		BindAddr:   server.RaftAddr,
		DataDir:    server.DataDir,
		Bootstrap:  false, // Never bootstrap on restart
		StorageAPI: api,
	}

	raftNode, err := raft.NewNode(raftConfig)
	require.NoError(tc.t, err, "failed to recreate RAFT node for %s", server.NodeID)
	server.RaftNode = raftNode

	// Create new context
	ctx, cancel := context.WithCancel(context.Background())
	server.ctx = ctx
	server.cancel = cancel
	server.serverErr = make(chan error, 1)

	// Start server
	tc.startServer(server)

	// Wait a bit for node to join
	time.Sleep(2 * time.Second)

	tc.t.Logf("Restarted node %s (index %d)", server.NodeID, nodeIndex)
}

// GetLeader returns the current leader node
func (tc *TestCluster) GetLeader() *TestServer {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	for _, server := range tc.servers {
		if server != nil && server.RaftNode != nil && server.RaftNode.IsLeader() {
			return server
		}
	}
	return nil
}

// waitForLeader waits for a leader to be elected
func (tc *TestCluster) waitForLeader(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		leader := tc.GetLeader()
		if leader != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Fail(t, "no leader elected within timeout")
}

// GetClient returns a gRPC client connected to a specific node
func (tc *TestCluster) GetClient(nodeIndex int) (*client.Client, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	require.GreaterOrEqual(tc.t, nodeIndex, 0, "nodeIndex must be >= 0")
	require.Less(tc.t, nodeIndex, len(tc.servers), "nodeIndex out of range")

	server := tc.servers[nodeIndex]
	require.NotNil(tc.t, server, "server at index %d is nil", nodeIndex)

	return client.NewClient(server.GrpcAddr)
}

// GetClientForLeader returns a gRPC client connected to the leader
func (tc *TestCluster) GetClientForLeader() (*client.Client, error) {
	leader := tc.GetLeader()
	require.NotNil(tc.t, leader, "no leader available")
	return client.NewClient(leader.GrpcAddr)
}

// GetClientForFollower returns a gRPC client connected to a follower
func (tc *TestCluster) GetClientForFollower() (*client.Client, error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	for _, server := range tc.servers {
		if server != nil && server.RaftNode != nil && !server.RaftNode.IsLeader() {
			return client.NewClient(server.GrpcAddr)
		}
	}
	return nil, fmt.Errorf("no follower available")
}

// GetServer returns a server by index
func (tc *TestCluster) GetServer(nodeIndex int) *TestServer {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	if nodeIndex < 0 || nodeIndex >= len(tc.servers) {
		return nil
	}
	return tc.servers[nodeIndex]
}

// Shutdown stops all nodes in the cluster
func (tc *TestCluster) Shutdown() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	for i := range tc.servers {
		if tc.servers[i] != nil {
			tc.servers[i].cancel()
			if tc.servers[i].RaftNode != nil {
				tc.servers[i].RaftNode.Close()
			}
			if tc.servers[i].StorageAPI != nil {
				tc.servers[i].StorageAPI.Close()
			}
			tc.servers[i].wg.Wait()
		}
	}
}

// getFreePort finds a free port
func getFreePort(t *testing.T) int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	require.NoError(t, err)

	listener, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

// Test_BasicWriteRead tests basic write and read operations with replication
func Test_BasicWriteRead(t *testing.T) {
	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	// Get leader client
	leaderClient, err := cluster.GetClientForLeader()
	require.NoError(t, err)
	defer leaderClient.Close()

	// Write to leader
	key := "alpha"
	value := []byte("beta")
	err = leaderClient.Put(context.Background(), key, value)
	require.NoError(t, err, "failed to write to leader")

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Read from follower
	followerClient, err := cluster.GetClientForFollower()
	require.NoError(t, err)
	defer followerClient.Close()

	// Retry reading from follower (replication might take a moment)
	var readValue []byte
	for i := 0; i < 10; i++ {
		readValue, err = followerClient.Get(context.Background(), key)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NoError(t, err, "failed to read from follower after retries")
	require.Equal(t, value, readValue, "value read from follower should match written value")
}

// Test_LeaderFailure_DataPersistence tests leader failure and data persistence
func Test_LeaderFailure_DataPersistence(t *testing.T) {
	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	// Get initial leader
	initialLeader := cluster.GetLeader()
	require.NotNil(t, initialLeader, "should have an initial leader")

	initialLeaderIndex := -1
	for i, s := range cluster.servers {
		if s.NodeID == initialLeader.NodeID {
			initialLeaderIndex = i
			break
		}
	}
	require.GreaterOrEqual(t, initialLeaderIndex, 0, "should find initial leader index")

	// Get client for initial leader
	leaderClient, err := cluster.GetClientForLeader()
	require.NoError(t, err)
	defer leaderClient.Close()

	// Write first key-value pair
	key1 := "resilience"
	value1 := []byte("test")
	err = leaderClient.Put(context.Background(), key1, value1)
	require.NoError(t, err, "failed to write first key-value pair")

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Stop the leader (simulate crash)
	t.Logf("Stopping leader node %s (index %d)", initialLeader.NodeID, initialLeaderIndex)
	cluster.StopNode(initialLeaderIndex)

	// Wait for new leader election
	t.Log("Waiting for new leader election...")
	cluster.waitForLeader(t)
	time.Sleep(2 * time.Second)

	// Verify new leader is different
	newLeader := cluster.GetLeader()
	require.NotNil(t, newLeader, "should have a new leader")
	require.NotEqual(t, initialLeader.NodeID, newLeader.NodeID, "new leader should be different from old leader")

	// Get client for new leader
	newLeaderClient, err := cluster.GetClientForLeader()
	require.NoError(t, err)
	defer newLeaderClient.Close()

	// Verify we can still read the first key from new leader
	readValue1, err := newLeaderClient.Get(context.Background(), key1)
	require.NoError(t, err, "should be able to read first key from new leader")
	require.Equal(t, value1, readValue1, "first key value should match")

	// Write a new key-value pair to the new leader
	key2 := "after_crash"
	value2 := []byte("success")
	err = newLeaderClient.Put(context.Background(), key2, value2)
	require.NoError(t, err, "failed to write second key-value pair to new leader")

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Restart the old leader
	t.Logf("Restarting old leader node %s (index %d)", initialLeader.NodeID, initialLeaderIndex)
	cluster.StartNode(initialLeaderIndex)

	// Wait for the old leader to catch up
	t.Log("Waiting for old leader to catch up...")
	time.Sleep(3 * time.Second)

	// Get client for restarted node
	restartedClient, err := cluster.GetClient(initialLeaderIndex)
	require.NoError(t, err)
	defer restartedClient.Close()

	// Verify the restarted node has both keys
	// Retry reading (node might still be catching up)
	var readValue1Restarted []byte
	var readValue2Restarted []byte
	for i := 0; i < 20; i++ {
		readValue1Restarted, err = restartedClient.Get(context.Background(), key1)
		if err == nil {
			readValue2Restarted, err = restartedClient.Get(context.Background(), key2)
			if err == nil {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	require.NoError(t, err, "should be able to read keys from restarted node")
	require.Equal(t, value1, readValue1Restarted, "first key should match on restarted node")
	require.Equal(t, value2, readValue2Restarted, "second key should match on restarted node")

	t.Log("✓ Old leader successfully caught up with both keys")
}

// Test_MultipleWrites tests multiple writes and reads across the cluster
func Test_MultipleWrites(t *testing.T) {
	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	leaderClient, err := cluster.GetClientForLeader()
	require.NoError(t, err)
	defer leaderClient.Close()

	// Write multiple key-value pairs
	testData := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	for key, value := range testData {
		err = leaderClient.Put(context.Background(), key, value)
		require.NoError(t, err, "failed to write %s", key)
	}

	// Wait for replication
	time.Sleep(2 * time.Second)

	// Verify all nodes have the data
	for i := 0; i < len(cluster.servers); i++ {
		client, err := cluster.GetClient(i)
		require.NoError(t, err)

		for key, expectedValue := range testData {
			value, err := client.Get(context.Background(), key)
			require.NoError(t, err, "node %d should have key %s", i, key)
			require.Equal(t, expectedValue, value, "node %d should have correct value for %s", i, key)
		}
		client.Close()
	}
}

