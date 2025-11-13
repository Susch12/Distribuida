package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"pubsub/proto"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ProcessingPattern string

const (
	FastPattern   ProcessingPattern = "fast"
	SlowPattern   ProcessingPattern = "slow"
	NormalPattern ProcessingPattern = "normal"
)

type Client struct {
	id                int32
	serverAddr        string
	conn              *grpc.ClientConn
	client            proto.PubSubServiceClient
	queues            []string
	processedMsgs     int
	maxMessages       int
	duration          time.Duration
	pattern           ProcessingPattern
	startTime         time.Time
	shouldStop        bool
	maxReconnectDelay time.Duration
}

func NewClient(id int32, serverAddr string, maxMessages int, duration time.Duration, pattern ProcessingPattern) *Client {
	return &Client{
		id:                id,
		serverAddr:        serverAddr,
		processedMsgs:     0,
		maxMessages:       maxMessages,
		duration:          duration,
		pattern:           pattern,
		startTime:         time.Now(),
		shouldStop:        false,
		maxReconnectDelay: 30 * time.Second,
	}
}

func (c *Client) Connect() error {
	return c.connectWithRetry()
}

func (c *Client) connectWithRetry() error {
	backoff := 1 * time.Second
	attempts := 0

	for {
		var err error
		c.conn, err = grpc.NewClient(c.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			c.client = proto.NewPubSubServiceClient(c.conn)
			log.Printf("Client %d connected to server at %s", c.id, c.serverAddr)
			return nil
		}

		attempts++
		log.Printf("Client %d connection attempt %d failed: %v. Retrying in %v...", c.id, attempts, err, backoff)
		time.Sleep(backoff)

		// Exponential backoff with max delay
		backoff *= 2
		if backoff > c.maxReconnectDelay {
			backoff = c.maxReconnectDelay
		}

		// Give up after 10 attempts
		if attempts >= 10 {
			return fmt.Errorf("failed to connect after %d attempts", attempts)
		}
	}
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
	// Simulate different processing patterns
	switch c.pattern {
	case FastPattern:
		time.Sleep(1 * time.Millisecond)
	case SlowPattern:
		time.Sleep(50 * time.Millisecond)
	case NormalPattern:
		time.Sleep(10 * time.Millisecond)
	}

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

// shouldStopProcessing checks if client should stop based on configuration
func (c *Client) shouldStopProcessing() bool {
	if c.shouldStop {
		return true
	}

	// Check max messages limit
	if c.maxMessages > 0 && c.processedMsgs >= c.maxMessages {
		log.Printf("Client %d reached max messages limit (%d)", c.id, c.maxMessages)
		return true
	}

	// Check duration limit
	if c.duration > 0 && time.Since(c.startTime) >= c.duration {
		log.Printf("Client %d reached duration limit (%v)", c.id, c.duration)
		return true
	}

	return false
}

// Run starts the client's main loop with reconnection support
func (c *Client) Run(ctx context.Context) error {
	// Select which queues to subscribe to
	c.selectQueues()

	for {
		if c.shouldStopProcessing() {
			log.Printf("Client %d stopping. Processed %d messages", c.id, c.processedMsgs)
			return nil
		}

		// Try to run the subscription loop
		err := c.runSubscriptionLoop(ctx)
		if err == nil {
			return nil // Clean shutdown
		}

		// Check if we should stop
		if c.shouldStop {
			return err
		}

		// Try to reconnect
		log.Printf("Client %d attempting to reconnect...", c.id)
		c.Close()
		if err := c.connectWithRetry(); err != nil {
			log.Printf("Client %d failed to reconnect: %v", c.id, err)
			return err
		}
	}
}

// runSubscriptionLoop handles the subscription and message processing
func (c *Client) runSubscriptionLoop(ctx context.Context) error {
	stream, err := c.client.Subscribe(ctx, &proto.SubscribeRequest{
		ClientId: c.id,
		Queues:   c.queues,
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe: %v", err)
	}

	log.Printf("Client %d started receiving messages (pattern: %s)", c.id, c.pattern)

	// Receive and process messages
	for {
		if c.shouldStopProcessing() {
			return nil
		}

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
	}
}

func main() {
	clientID := flag.Int("id", 1, "Client ID")
	serverAddr := flag.String("server", "localhost:50051", "Server address")
	maxMessages := flag.Int("max-messages", 0, "Maximum number of messages to process (0 = unlimited)")
	durationSec := flag.Int("duration", 0, "Duration to run in seconds (0 = unlimited)")
	patternStr := flag.String("pattern", "normal", "Processing pattern: fast, normal, slow")
	flag.Parse()

	// Validate pattern
	pattern := ProcessingPattern(*patternStr)
	if pattern != FastPattern && pattern != NormalPattern && pattern != SlowPattern {
		log.Fatalf("Invalid pattern: %s. Use: fast, normal, or slow", *patternStr)
	}

	duration := time.Duration(*durationSec) * time.Second

	rand.Seed(time.Now().UnixNano() + int64(*clientID))

	client := NewClient(int32(*clientID), *serverAddr, *maxMessages, duration, pattern)
	defer client.Close()

	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run client in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- client.Run(ctx)
	}()

	// Wait for either error or shutdown signal
	select {
	case err := <-errChan:
		if err != nil {
			log.Printf("Client error: %v", err)
		}
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
		client.shouldStop = true
		cancel()
		// Wait a bit for graceful shutdown
		time.Sleep(1 * time.Second)
	}

	log.Printf("Client %d processed %d messages total", *clientID, client.processedMsgs)
}
