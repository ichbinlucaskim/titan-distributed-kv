package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	"github.com/hashicorp/raft-boltdb/v2"
	"titan-kv/storage"
)

// Node wraps a RAFT node and provides methods to interact with it
type Node struct {
	raft      *raft.Raft
	fsm       *FSM
	transport *raft.NetworkTransport
}

// Config holds configuration for a RAFT node
type Config struct {
	NodeID      string
	BindAddr    string
	DataDir     string
	Bootstrap   bool
	Peers       []string // Initial peer addresses for bootstrap
	StorageAPI  *storage.API
}

// NewNode creates and initializes a new RAFT node
func NewNode(config Config) (*Node, error) {
	// Create FSM
	fsm := NewFSM(config.StorageAPI)

	// Create RAFT configuration
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)
	raftConfig.SnapshotInterval = 30 * time.Second
	raftConfig.SnapshotThreshold = 2

	// Create transport
	addr, err := net.ResolveTCPAddr("tcp", config.BindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	transport, err := raft.NewTCPTransport(config.BindAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Create log store
	logStorePath := filepath.Join(config.DataDir, "raft", "log")
	if err := os.MkdirAll(logStorePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log store directory: %w", err)
	}

	logStore, err := raftboltdb.NewBoltStore(filepath.Join(logStorePath, "raft.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create log store: %w", err)
	}

	// Create stable store
	stableStorePath := filepath.Join(config.DataDir, "raft", "stable")
	if err := os.MkdirAll(stableStorePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stable store directory: %w", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(stableStorePath, "raft.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create stable store: %w", err)
	}

	// Create snapshot store
	snapshotStorePath := filepath.Join(config.DataDir, "raft", "snapshots")
	if err := os.MkdirAll(snapshotStorePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot store directory: %w", err)
	}

	snapshotStore, err := raft.NewFileSnapshotStore(snapshotStorePath, 2, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create RAFT instance
	raftNode, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft: %w", err)
	}

	node := &Node{
		raft:      raftNode,
		fsm:       fsm,
		transport: transport,
		store:     nil, // We don't need to store this reference
	}

	// Bootstrap if needed
	if config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(config.NodeID),
					Address: raft.ServerAddress(config.BindAddr),
				},
			},
		}
		future := raftNode.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
		}
	}

	return node, nil
}

// ApplyLogEntry applies a log entry through RAFT consensus
func (n *Node) ApplyLogEntry(entry LogEntry) error {
	if n.raft.State() != raft.Leader {
		return fmt.Errorf("not the leader")
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	future := n.raft.Apply(data, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply log entry: %w", err)
	}

	// Check if the response indicates an error
	if resp := future.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	return nil
}

// IsLeader returns true if this node is the leader
func (n *Node) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

// Leader returns the address of the current leader
func (n *Node) Leader() string {
	return string(n.raft.Leader())
}

// State returns the current RAFT state
func (n *Node) State() raft.RaftState {
	return n.raft.State()
}

// AddPeer adds a peer to the cluster
func (n *Node) AddPeer(peerID, peerAddr string) error {
	if !n.IsLeader() {
		return fmt.Errorf("only leader can add peers")
	}

	future := n.raft.AddVoter(raft.ServerID(peerID), raft.ServerAddress(peerAddr), 0, 0)
	return future.Error()
}

// RemovePeer removes a peer from the cluster
func (n *Node) RemovePeer(peerID string) error {
	if !n.IsLeader() {
		return fmt.Errorf("only leader can remove peers")
	}

	future := n.raft.RemoveServer(raft.ServerID(peerID), 0, 0)
	return future.Error()
}

// GetLastIndex returns the last applied log index
func (n *Node) GetLastIndex() uint64 {
	return n.fsm.GetLastIndex()
}

// Shutdown gracefully shuts down the RAFT node
func (n *Node) Shutdown() error {
	future := n.raft.Shutdown()
	return future.Error()
}

// Close closes the node and releases resources
func (n *Node) Close() error {
	if err := n.Shutdown(); err != nil {
		return err
	}
	if n.transport != nil {
		if closer, ok := n.transport.(io.Closer); ok {
			return closer.Close()
		}
	}
	return nil
}

