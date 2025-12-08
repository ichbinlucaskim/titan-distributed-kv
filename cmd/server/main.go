package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"titan-kv/raft"
	"titan-kv/server"
	"titan-kv/sharding"
	"titan-kv/storage"
)

func main() {
	grpcAddr := flag.String("grpc-addr", ":50051", "gRPC server address")
	raftAddr := flag.String("raft-addr", ":12000", "RAFT node address")
	nodeID := flag.String("node-id", "node1", "RAFT node ID")
	dataDir := flag.String("data-dir", "./data", "Directory for data files")
	bootstrap := flag.Bool("bootstrap", false, "Bootstrap the RAFT cluster")
	peers := flag.String("peers", "", "Comma-separated list of peer addresses (format: nodeID:grpcAddr:raftAddr)")
	flag.Parse()

	// Create storage engine
	bc, err := storage.NewBitcask(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create Bitcask: %v", err)
	}
	defer bc.Close()

	// Create compactor
	compactor := storage.NewCompactor(bc)

	// Create API
	api := storage.NewAPI(bc, compactor)

	// Create RAFT node
	raftConfig := raft.Config{
		NodeID:     *nodeID,
		BindAddr:   *raftAddr,
		DataDir:    *dataDir,
		Bootstrap:  *bootstrap,
		StorageAPI: api,
	}

	raftNode, err := raft.NewNode(raftConfig)
	if err != nil {
		log.Fatalf("Failed to create RAFT node: %v", err)
	}
	defer raftNode.Close()

	// Create membership service
	membership := sharding.NewMembershipService(*nodeID, *grpcAddr)

	// Add local node to membership
	if err := membership.AddNode(*nodeID, *grpcAddr, *raftAddr); err != nil {
		log.Fatalf("Failed to add local node to membership: %v", err)
	}

	// Parse and add peer nodes
	// Format: nodeID|grpcAddr|raftAddr,nodeID|grpcAddr|raftAddr,...
	if *peers != "" {
		peerList := strings.Split(*peers, ",")
		for _, peer := range peerList {
			peer = strings.TrimSpace(peer)
			if peer == "" {
				continue
			}
			parts := strings.Split(peer, "|")
			if len(parts) != 3 {
				log.Printf("Invalid peer format: %s (expected nodeID|grpcAddr|raftAddr)", peer)
				continue
			}
			peerID := parts[0]
			peerGrpcAddr := parts[1]
			peerRaftAddr := parts[2]

			// Add peer to membership
			if err := membership.AddNode(peerID, peerGrpcAddr, peerRaftAddr); err != nil {
				log.Printf("Failed to add peer %s: %v", peerID, err)
			} else {
				log.Printf("Added peer: %s (%s)", peerID, peerGrpcAddr)
			}
		}
	}

	// Start membership sync (refresh client connections periodically)
	membership.StartMembershipSync(30 * time.Second)

	// Update leader status periodically
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			isLeader := raftNode.IsLeader()
			membership.UpdateLeader(*nodeID, isLeader)
		}
	}()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal...")
		membership.Close()
		cancel()
	}()

	// Start sharded gRPC server
	log.Printf("Starting Titan-KV sharded gRPC server on %s", *grpcAddr)
	log.Printf("RAFT node ID: %s, RAFT address: %s", *nodeID, *raftAddr)
	log.Printf("Cluster nodes: %d", len(membership.GetAllNodes()))
	if err := server.StartShardedServerWithContext(ctx, api, raftNode, membership, *grpcAddr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}

	log.Println("Server stopped")
}
