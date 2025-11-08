package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"pubsub/proto"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	id            int32
	serverAddr    string
	conn          *grpc.ClientConn
	client        proto.PubSubServiceClient
	queues        []string
	processedMsgs int
}

func NewClient(id int32, serverAddr string) *Client {
	return &Client{
		id:            id,
		serverAddr:    serverAddr,
		processedMsgs: 0,
	}
}

func (c *Client) Connect() error {
	var err error
	c.conn, err = grpc.NewClient(c.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}

	c.client = proto.NewPubSubServiceClient(c.conn)
	log.Printf("Client %d connected to server at %s", c.id, c.serverAddr)
	return nil
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// selectQueues randomly decides if client subscribes to 1 or 2 queues
func (c *Client) selectQueues() {
	allQueues := []string{"primary", "secondary", "tertiary"}

	// 50% chance to subscribe to 1 queue, 50% chance to subscribe to 2 queues
	if rand.Intn(2) == 0 {
		// Subscribe to 1 queue
		queue := allQueues[rand.Intn(3)]
		c.queues = []string{queue}
		log.Printf("Client %d subscribing to 1 queue: %v", c.id, c.queues)
	} else {
		// Subscribe to 2 different queues
		// Randomly select 2 different queues
		first := rand.Intn(3)
		second := (first + 1 + rand.Intn(2)) % 3
		c.queues = []string{allQueues[first], allQueues[second]}
		log.Printf("Client %d subscribing to 2 queues: %v", c.id, c.queues)
	}
}

// processNumbers calculates the sum of the numbers in the set
func (c *Client) processNumbers(numbers []int32) int32 {
	var sum int32 = 0
	for _, num := range numbers {
		sum += num
	}
	return sum
}

// sendResult sends the processed result back to the server
func (c *Client) sendResult(messageID int32, result int32) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.SendResult(ctx, &proto.ResultRequest{
		ClientId:  c.id,
		MessageId: messageID,
		Result:    result,
	})

	if err != nil {
		return fmt.Errorf("failed to send result: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("server rejected result")
	}

	return nil
}

// Run starts the client's main loop
func (c *Client) Run() error {
	// Select which queues to subscribe to
	c.selectQueues()

	// Subscribe to queues
	ctx := context.Background()
	stream, err := c.client.Subscribe(ctx, &proto.SubscribeRequest{
		ClientId: c.id,
		Queues:   c.queues,
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe: %v", err)
	}

	log.Printf("Client %d started receiving messages", c.id)

	// Receive and process messages
	for {
		msg, err := stream.Recv()
		if err != nil {
			log.Printf("Client %d stream error: %v", c.id, err)
			return err
		}

		// Process the number set
		result := c.processNumbers(msg.Numbers)
		c.processedMsgs++

		log.Printf("Client %d received msg %d from queue %s: %v -> result: %d",
			c.id, msg.MessageId, msg.Queue, msg.Numbers, result)

		// Send result back to server
		if err := c.sendResult(msg.MessageId, result); err != nil {
			log.Printf("Client %d failed to send result: %v", c.id, err)
			continue
		}

		// Small delay to simulate processing time
		time.Sleep(10 * time.Millisecond)
	}
}

func main() {
	clientID := flag.Int("id", 1, "Client ID")
	serverAddr := flag.String("server", "localhost:50051", "Server address")
	flag.Parse()

	rand.Seed(time.Now().UnixNano() + int64(*clientID))

	client := NewClient(int32(*clientID), *serverAddr)
	defer client.Close()

	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	if err := client.Run(); err != nil {
		log.Fatalf("Client error: %v", err)
	}
}
