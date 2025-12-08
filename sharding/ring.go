package sharding

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
)

// HashRing implements consistent hashing for key-to-node mapping
type HashRing struct {
	mu          sync.RWMutex
	nodes       map[string]*NodeInfo // nodeID -> NodeInfo
	sortedKeys  []uint32             // Sorted hash keys for binary search
	replicas    int                  // Number of virtual nodes per physical node
}

// NodeInfo contains information about a node in the cluster
type NodeInfo struct {
	NodeID    string
	Address   string // gRPC address
	RaftAddr  string // RAFT address
	IsLeader  bool   // Whether this node is the RAFT leader for its shard
}

// NewHashRing creates a new consistent hashing ring
func NewHashRing(replicas int) *HashRing {
	if replicas <= 0 {
		replicas = 150 // Default number of virtual nodes
	}
	return &HashRing{
		nodes:      make(map[string]*NodeInfo),
		sortedKeys: make([]uint32, 0),
		replicas:   replicas,
	}
}

// hashKey computes a hash for a given key
func (r *HashRing) hashKey(key string) uint32 {
	h := sha256.Sum256([]byte(key))
	// Use first 4 bytes as uint32
	return uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
}

// hashNode computes a hash for a node (for virtual nodes)
func (r *HashRing) hashNode(nodeID string, replica int) uint32 {
	key := fmt.Sprintf("%s:%d", nodeID, replica)
	return r.hashKey(key)
}

// AddNode adds a node to the hash ring
func (r *HashRing) AddNode(nodeID, address, raftAddr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[nodeID]; exists {
		return // Node already exists
	}

	r.nodes[nodeID] = &NodeInfo{
		NodeID:   nodeID,
		Address:  address,
		RaftAddr: raftAddr,
		IsLeader: false,
	}

	// Rebuild sorted keys
	r.rebuildSortedKeys()
}

// RemoveNode removes a node from the hash ring
func (r *HashRing) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[nodeID]; !exists {
		return // Node doesn't exist
	}

	delete(r.nodes, nodeID)
	r.rebuildSortedKeys()
}

// rebuildSortedKeys rebuilds the sorted keys array
func (r *HashRing) rebuildSortedKeys() {
	r.sortedKeys = make([]uint32, 0, len(r.nodes)*r.replicas)
	for nodeID := range r.nodes {
		for i := 0; i < r.replicas; i++ {
			hash := r.hashNode(nodeID, i)
			r.sortedKeys = append(r.sortedKeys, hash)
		}
	}
	sort.Slice(r.sortedKeys, func(i, j int) bool {
		return r.sortedKeys[i] < r.sortedKeys[j]
	})
}

// GetNode returns the node responsible for a given key
func (r *HashRing) GetNode(key string) (*NodeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.nodes) == 0 {
		return nil, fmt.Errorf("no nodes in ring")
	}

	keyHash := r.hashKey(key)

	// Binary search for the first node with hash >= keyHash
	idx := sort.Search(len(r.sortedKeys), func(i int) bool {
		return r.sortedKeys[i] >= keyHash
	})

	// Wrap around if we've reached the end
	if idx >= len(r.sortedKeys) {
		idx = 0
	}

	// Find which node this hash belongs to
	targetHash := r.sortedKeys[idx]
	for nodeID, nodeInfo := range r.nodes {
		for i := 0; i < r.replicas; i++ {
			if r.hashNode(nodeID, i) == targetHash {
				return nodeInfo, nil
			}
		}
	}

	// Fallback: return first node (shouldn't happen)
	for _, nodeInfo := range r.nodes {
		return nodeInfo, nil
	}

	return nil, fmt.Errorf("failed to find node for key")
}

// GetReplicas returns N replicas (nodes) for a given key
func (r *HashRing) GetReplicas(key string, n int) ([]*NodeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.nodes) == 0 {
		return nil, fmt.Errorf("no nodes in ring")
	}

	if n > len(r.nodes) {
		n = len(r.nodes)
	}

	keyHash := r.hashKey(key)
	idx := sort.Search(len(r.sortedKeys), func(i int) bool {
		return r.sortedKeys[i] >= keyHash
	})

	if idx >= len(r.sortedKeys) {
		idx = 0
	}

	replicas := make([]*NodeInfo, 0, n)
	seen := make(map[string]bool)

	for len(replicas) < n {
		targetHash := r.sortedKeys[idx]
		
		// Find node for this hash
		for nodeID, nodeInfo := range r.nodes {
			if seen[nodeID] {
				continue
			}
			for i := 0; i < r.replicas; i++ {
				if r.hashNode(nodeID, i) == targetHash {
					replicas = append(replicas, nodeInfo)
					seen[nodeID] = true
					break
				}
			}
			if seen[nodeID] {
				break
			}
		}

		idx = (idx + 1) % len(r.sortedKeys)
		
		// Prevent infinite loop
		if len(replicas) == 0 && idx == sort.Search(len(r.sortedKeys), func(i int) bool {
			return r.sortedKeys[i] >= keyHash
		}) {
			break
		}
	}

	return replicas, nil
}

// GetAllNodes returns all nodes in the ring
func (r *HashRing) GetAllNodes() []*NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]*NodeInfo, 0, len(r.nodes))
	for _, nodeInfo := range r.nodes {
		nodes = append(nodes, nodeInfo)
	}
	return nodes
}

// UpdateNodeLeader updates the leader status for a node
func (r *HashRing) UpdateNodeLeader(nodeID string, isLeader bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if nodeInfo, exists := r.nodes[nodeID]; exists {
		nodeInfo.IsLeader = isLeader
	}
}

// GetNodeByID returns node information by node ID
func (r *HashRing) GetNodeByID(nodeID string) (*NodeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeInfo, exists := r.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}
	return nodeInfo, nil
}

