package raft

import (
	"fmt"
	"time"

	"github.com/hashicorp/raft"
)

// JoinCluster adds a new node to an existing RAFT cluster
// This should be called on the leader node
func (n *Node) JoinCluster(nodeID, nodeAddr string) error {
	if !n.IsLeader() {
		return fmt.Errorf("only leader can add nodes to cluster")
	}

	future := n.raft.AddVoter(
		raft.ServerID(nodeID),
		raft.ServerAddress(nodeAddr),
		0, // No timeout
		0, // No timeout
	)

	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add voter: %w", err)
	}

	return nil
}

// BootstrapClusterWithPeers bootstraps a cluster with multiple nodes
// This should be called on the first node
func BootstrapClusterWithPeers(raftNode *raft.Raft, nodeID, nodeAddr string, peers map[string]string) error {
	servers := []raft.Server{
		{
			ID:      raft.ServerID(nodeID),
			Address: raft.ServerAddress(nodeAddr),
		},
	}

	for peerID, peerAddr := range peers {
		servers = append(servers, raft.Server{
			ID:      raft.ServerID(peerID),
			Address: raft.ServerAddress(peerAddr),
		})
	}

	configuration := raft.Configuration{
		Servers: servers,
	}

	future := raftNode.BootstrapCluster(configuration)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	// Wait a bit for the cluster to stabilize
	time.Sleep(1 * time.Second)

	return nil
}

