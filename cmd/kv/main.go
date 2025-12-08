package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"titan-kv/storage"
)

func main() {
	dataDir := flag.String("data-dir", "./data", "Directory for data files")
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

	// Run periodic compaction in background
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // Compact every 5 minutes
		defer ticker.Stop()

		for range ticker.C {
			if !compactor.IsActive() {
				fmt.Println("Starting compaction...")
				if err := compactor.Compact(); err != nil {
					log.Printf("Compaction failed: %v", err)
				} else {
					fmt.Println("Compaction completed")
				}
			}
		}
	}()

	// Example usage
	fmt.Println("Titan-KV Storage Engine Demo")
	fmt.Println("============================")

	// PUT operations
	fmt.Println("\n1. PUT operations:")
	if err := api.Put("user:1", []byte("Alice")); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println("  PUT user:1 = Alice")
	}

	if err := api.Put("user:2", []byte("Bob")); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println("  PUT user:2 = Bob")
	}

	if err := api.Put("user:1", []byte("Alice Updated")); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println("  PUT user:1 = Alice Updated (update)")
	}

	// GET operations
	fmt.Println("\n2. GET operations:")
	if value, err := api.Get("user:1"); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("  GET user:1 = %s\n", string(value))
	}

	if value, err := api.Get("user:2"); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("  GET user:2 = %s\n", string(value))
	}

	if _, err := api.Get("user:999"); err != nil {
		fmt.Printf("  GET user:999 = Error: %v (expected)\n", err)
	}

	// DELETE operation
	fmt.Println("\n3. DELETE operation:")
	if err := api.Delete("user:2"); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Println("  DELETE user:2")
	}

	if _, err := api.Get("user:2"); err != nil {
		fmt.Printf("  GET user:2 = Error: %v (expected after delete)\n", err)
	}

	fmt.Println("\nStorage engine is running. Press Ctrl+C to exit.")
	fmt.Println("Compaction will run automatically every 5 minutes.")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
}

