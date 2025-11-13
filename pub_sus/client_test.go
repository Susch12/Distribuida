package main

import (
	"testing"
	"time"
)

func TestProcessNumbers(t *testing.T) {
	client := NewClient(1, "localhost:50051", 0, 0, FastPattern)

	tests := []struct {
		name     string
		numbers  []int32
		expected int32
	}{
		{"Two numbers", []int32{5, 10}, 15},
		{"Three numbers", []int32{1, 2, 3}, 6},
		{"Single number", []int32{42}, 42},
		{"Zeros", []int32{0, 0, 0}, 0},
		{"Negative numbers", []int32{-5, 10, -3}, 2},
		{"Large numbers", []int32{1000, 2000, 3000}, 6000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.processNumbers(tt.numbers)
			if result != tt.expected {
				t.Errorf("processNumbers(%v) = %d, expected %d", tt.numbers, result, tt.expected)
			}
		})
	}
}

func TestProcessingPatterns(t *testing.T) {
	tests := []struct {
		name            string
		pattern         ProcessingPattern
		maxDuration     time.Duration
		minDuration     time.Duration
	}{
		{"Fast pattern", FastPattern, 5 * time.Millisecond, 1 * time.Millisecond},
		{"Normal pattern", NormalPattern, 15 * time.Millisecond, 10 * time.Millisecond},
		{"Slow pattern", SlowPattern, 60 * time.Millisecond, 50 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(1, "localhost:50051", 0, 0, tt.pattern)
			numbers := []int32{1, 2, 3}

			start := time.Now()
			client.processNumbers(numbers)
			duration := time.Since(start)

			if duration < tt.minDuration || duration > tt.maxDuration {
				t.Errorf("Processing took %v, expected between %v and %v",
					duration, tt.minDuration, tt.maxDuration)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	id := int32(123)
	serverAddr := "localhost:50051"
	maxMessages := 100
	duration := 60 * time.Second
	pattern := FastPattern

	client := NewClient(id, serverAddr, maxMessages, duration, pattern)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.id != id {
		t.Errorf("id = %d, expected %d", client.id, id)
	}

	if client.serverAddr != serverAddr {
		t.Errorf("serverAddr = %s, expected %s", client.serverAddr, serverAddr)
	}

	if client.maxMessages != maxMessages {
		t.Errorf("maxMessages = %d, expected %d", client.maxMessages, maxMessages)
	}

	if client.duration != duration {
		t.Errorf("duration = %v, expected %v", client.duration, duration)
	}

	if client.pattern != pattern {
		t.Errorf("pattern = %v, expected %v", client.pattern, pattern)
	}

	if client.processedMsgs != 0 {
		t.Errorf("processedMsgs = %d, expected 0", client.processedMsgs)
	}

	if client.shouldStop != false {
		t.Errorf("shouldStop = %v, expected false", client.shouldStop)
	}
}

func TestShouldStopProcessing(t *testing.T) {
	tests := []struct {
		name            string
		maxMessages     int
		processedMsgs   int
		duration        time.Duration
		elapsed         time.Duration
		shouldStop      bool
		expectedResult  bool
	}{
		{"No limits", 0, 10, 0, 5 * time.Second, false, false},
		{"Max messages reached", 100, 100, 0, 0, false, true},
		{"Max messages not reached", 100, 50, 0, 0, false, false},
		{"Duration reached", 0, 10, 10 * time.Second, 11 * time.Second, false, true},
		{"Duration not reached", 0, 10, 10 * time.Second, 5 * time.Second, false, false},
		{"Should stop flag", 0, 10, 0, 0, true, true},
		{"Multiple conditions met", 100, 100, 10 * time.Second, 11 * time.Second, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(1, "localhost:50051", tt.maxMessages, tt.duration, NormalPattern)
			client.processedMsgs = tt.processedMsgs
			client.shouldStop = tt.shouldStop
			client.startTime = time.Now().Add(-tt.elapsed)

			result := client.shouldStopProcessing()
			if result != tt.expectedResult {
				t.Errorf("shouldStopProcessing() = %v, expected %v", result, tt.expectedResult)
			}
		})
	}
}

func TestSelectQueues(t *testing.T) {
	// Test multiple times to ensure both single and dual queue subscriptions occur
	singleQueueCount := 0
	dualQueueCount := 0
	iterations := 100

	for i := 0; i < iterations; i++ {
		client := NewClient(int32(i), "localhost:50051", 0, 0, NormalPattern)
		client.selectQueues()

		if len(client.queues) == 1 {
			singleQueueCount++
		} else if len(client.queues) == 2 {
			dualQueueCount++
			// Verify queues are different
			if client.queues[0] == client.queues[1] {
				t.Error("Dual queue subscription has duplicate queues")
			}
		} else {
			t.Errorf("Invalid number of queues: %d", len(client.queues))
		}

		// Verify queue names are valid
		for _, queue := range client.queues {
			if queue != "primary" && queue != "secondary" && queue != "tertiary" {
				t.Errorf("Invalid queue name: %s", queue)
			}
		}
	}

	// Both types should occur (allowing some variance due to randomness)
	if singleQueueCount < 20 || singleQueueCount > 80 {
		t.Errorf("Single queue subscription occurred %d times out of %d, expected around 50",
			singleQueueCount, iterations)
	}

	if dualQueueCount < 20 || dualQueueCount > 80 {
		t.Errorf("Dual queue subscription occurred %d times out of %d, expected around 50",
			dualQueueCount, iterations)
	}
}
