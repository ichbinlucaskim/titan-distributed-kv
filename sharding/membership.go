package sharding

import (
	"fmt"
	"sync"
	"time"

	"titan-kv/client"
)

// MembershipService manages cluster membership and the hash ring
type MembershipService struct {
	mu          sync.RWMutex
	ring        *HashRing
	localNodeID string
	localAddr   string
	clients     map[string]*client.Client // nodeID -> gRPC client
}

// NewMembershipService creates a new membership service
func NewMembershipService(localNodeID, localAddr string) *MembershipService {
	return &MembershipService{
		ring:        NewHashRing(150),
		localNodeID: localNodeID,
		localAddr:   localAddr,
		clients:     make(map[string]*client.Client),
	}
}

// AddNode adds a node to the cluster
func (m *MembershipService) AddNode(nodeID, address, raftAddr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add to hash ring
	m.ring.AddNode(nodeID, address, raftAddr)

	// Create gRPC client for this node if not local
	if nodeID != m.localNodeID {
		cli, err := client.NewClient(address)
		if err != nil {
			return fmt.Errorf("failed to create client for node %s: %w", nodeID, err)
		}
		m.clients[nodeID] = cli
	}

	return nil
}

// RemoveNode removes a node from the cluster
func (m *MembershipService) RemoveNode(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close client connection
	if cli, exists := m.clients[nodeID]; exists {
		cli.Close()
		delete(m.clients, nodeID)
	}

	// Remove from hash ring
	m.ring.RemoveNode(nodeID)
}

// GetTargetNode returns the node responsible for a given key
func (m *MembershipService) GetTargetNode(key string) (*NodeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.ring.GetNode(key)
}

// GetReplicas returns replica nodes for a key
func (m *MembershipService) GetReplicas(key string, n int) ([]*NodeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.ring.GetReplicas(key, n)
}

// GetClient returns the gRPC client for a node
func (m *MembershipService) GetClient(nodeID string) (*client.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if nodeID == m.localNodeID {
		return nil, fmt.Errorf("cannot get client for local node")
	}

	cli, exists := m.clients[nodeID]
	if !exists {
		return nil, fmt.Errorf("client for node %s not found", nodeID)
	}

	return cli, nil
}

// IsLocalNode checks if a node is the local node
func (m *MembershipService) IsLocalNode(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return nodeID == m.localNodeID
}

// UpdateLeader updates the leader status for a node
func (m *MembershipService) UpdateLeader(nodeID string, isLeader bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ring.UpdateNodeLeader(nodeID, isLeader)
}

// GetAllNodes returns all nodes in the cluster
func (m *MembershipService) GetAllNodes() []*NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ring.GetAllNodes()
}

// Close closes all client connections
func (m *MembershipService) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for nodeID, cli := range m.clients {
		cli.Close()
		delete(m.clients, nodeID)
	}
}

// RefreshClients refreshes client connections (useful for reconnection)
func (m *MembershipService) RefreshClients() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for nodeID, nodeInfo := range m.ring.GetAllNodes() {
		if nodeID == m.localNodeID {
			continue
		}

		// Close existing client if any
		if cli, exists := m.clients[nodeID]; exists {
			cli.Close()
		}

		// Create new client
		cli, err := client.NewClient(nodeInfo.Address)
		if err != nil {
			return fmt.Errorf("failed to refresh client for node %s: %w", nodeID, err)
		}
		m.clients[nodeID] = cli
	}

	return nil
}

// StartMembershipSync starts a background goroutine to sync membership
// This is a placeholder for future implementation
func (m *MembershipService) StartMembershipSync(syncInterval time.Duration) {
	go func() {
		ticker := time.NewTicker(syncInterval)
		defer ticker.Stop()

		for range ticker.C {
			// Refresh client connections
			if err := m.RefreshClients(); err != nil {
				// Log error but continue
				fmt.Printf("Error refreshing clients: %v\n", err)
			}
		}
	}()
}

