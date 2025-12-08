package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"titan-kv/client"
)

func main() {
	address := flag.String("address", "localhost:50051", "gRPC server address")
	flag.Parse()

	// Create client
	c, err := client.NewClient(*address)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	fmt.Println("Titan-KV CLI Client")
	fmt.Println("===================")
	fmt.Printf("Connected to server at %s\n", *address)
	fmt.Println("\nCommands:")
	fmt.Println("  PUT <key> <value>  - Store a key-value pair")
	fmt.Println("  GET <key>          - Retrieve a value by key")
	fmt.Println("  DELETE <key>       - Delete a key-value pair")
	fmt.Println("  QUIT               - Exit the client")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("titan-kv> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		command := strings.ToUpper(parts[0])

		switch command {
		case "PUT":
			if len(parts) < 3 {
				fmt.Println("Error: PUT requires key and value")
				fmt.Println("Usage: PUT <key> <value>")
				continue
			}
			key := parts[1]
			value := strings.Join(parts[2:], " ")
			if err := c.Put(nil, key, []byte(value)); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("OK: Stored key '%s'\n", key)
			}

		case "GET":
			if len(parts) < 2 {
				fmt.Println("Error: GET requires a key")
				fmt.Println("Usage: GET <key>")
				continue
			}
			key := parts[1]
			value, err := c.Get(nil, key)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("Value: %s\n", string(value))
			}

		case "DELETE":
			if len(parts) < 2 {
				fmt.Println("Error: DELETE requires a key")
				fmt.Println("Usage: DELETE <key>")
				continue
			}
			key := parts[1]
			if err := c.Delete(nil, key); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("OK: Deleted key '%s'\n", key)
			}

		case "QUIT", "EXIT":
			fmt.Println("Goodbye!")
			return

		case "HELP":
			fmt.Println("\nCommands:")
			fmt.Println("  PUT <key> <value>  - Store a key-value pair")
			fmt.Println("  GET <key>          - Retrieve a value by key")
			fmt.Println("  DELETE <key>       - Delete a key-value pair")
			fmt.Println("  QUIT               - Exit the client")
			fmt.Println()

		default:
			fmt.Printf("Unknown command: %s\n", command)
			fmt.Println("Type HELP for available commands")
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading input: %v", err)
	}
}

