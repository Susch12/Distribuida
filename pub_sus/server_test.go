package main

import (
	"testing"
)

func TestSelectRandomQueue(t *testing.T) {
	server := NewPubSubServer(RandomCriteria)

	// Run 1000 times to test distribution
	counts := make(map[QueueType]int)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		queue := server.selectRandomQueue()
		counts[queue]++
	}

	// Each queue should get roughly 33% (allowing 20% margin of error)
	expected := iterations / 3
	margin := expected / 5 // 20% margin

	for _, queueType := range []QueueType{PrimaryQueue, SecondaryQueue, TertiaryQueue} {
		count := counts[queueType]
		if count < expected-margin || count > expected+margin {
			t.Errorf("Queue %s: got %d, expected around %d (±%d)", queueType, count, expected, margin)
		}
	}
}

func TestSelectWeightedQueue(t *testing.T) {
	server := NewPubSubServer(WeightedCriteria)

	// Run 10000 times to test distribution
	counts := make(map[QueueType]int)
	iterations := 10000

	for i := 0; i < iterations; i++ {
		queue := server.selectWeightedQueue()
		counts[queue]++
	}

	// Test expected distributions with 10% margin
	tests := []struct {
		queue    QueueType
		expected float64
		margin   float64
	}{
		{PrimaryQueue, 0.50, 0.05},   // 50% ± 5%
		{SecondaryQueue, 0.30, 0.05}, // 30% ± 5%
		{TertiaryQueue, 0.20, 0.05},  // 20% ± 5%
	}

	for _, tt := range tests {
		count := counts[tt.queue]
		ratio := float64(count) / float64(iterations)
		if ratio < tt.expected-tt.margin || ratio > tt.expected+tt.margin {
			t.Errorf("Queue %s: got %.2f%%, expected %.2f%% (±%.2f%%)",
				tt.queue, ratio*100, tt.expected*100, tt.margin*100)
		}
	}
}

func TestSelectConditionalQueue(t *testing.T) {
	server := NewPubSubServer(ConditionalCriteria)

	tests := []struct {
		name     string
		numbers  []int
		expected QueueType
	}{
		{"Two even numbers", []int{2, 4}, PrimaryQueue},
		{"Two even numbers (larger)", []int{10, 20}, PrimaryQueue},
		{"Two odd numbers", []int{1, 3}, SecondaryQueue},
		{"Two odd numbers (larger)", []int{11, 33}, SecondaryQueue},
		{"Three even numbers", []int{2, 4, 6}, TertiaryQueue},
		{"Three odd numbers", []int{1, 3, 5}, TertiaryQueue},
		{"One even, one odd", []int{2, 3}, PrimaryQueue}, // Default case
		{"One even, two odd", []int{2, 3, 5}, SecondaryQueue},
		{"Two even, one odd", []int{2, 4, 5}, PrimaryQueue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.selectConditionalQueue(tt.numbers)
			if result != tt.expected {
				t.Errorf("selectConditionalQueue(%v) = %v, expected %v",
					tt.numbers, result, tt.expected)
			}
		})
	}
}

func TestSelectQueueWithCriteria(t *testing.T) {
	tests := []struct {
		name     string
		criteria SelectionCriteria
		numbers  []int
	}{
		{"Random criteria", RandomCriteria, []int{1, 2, 3}},
		{"Weighted criteria", WeightedCriteria, []int{1, 2, 3}},
		{"Conditional criteria", ConditionalCriteria, []int{2, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewPubSubServer(tt.criteria)
			queue := server.selectQueue(tt.numbers)

			// Just verify it returns a valid queue type
			if queue != PrimaryQueue && queue != SecondaryQueue && queue != TertiaryQueue {
				t.Errorf("selectQueue returned invalid queue: %v", queue)
			}
		})
	}
}

func TestNewPubSubServer(t *testing.T) {
	criteria := RandomCriteria
	server := NewPubSubServer(criteria)

	if server == nil {
		t.Fatal("NewPubSubServer returned nil")
	}

	if server.criteria != criteria {
		t.Errorf("criteria = %v, expected %v", server.criteria, criteria)
	}

	if server.messageID != 0 {
		t.Errorf("messageID = %d, expected 0", server.messageID)
	}

	if server.stopPublishing != false {
		t.Errorf("stopPublishing = %v, expected false", server.stopPublishing)
	}

	if server.primaryQueue == nil || server.secondaryQueue == nil || server.tertiaryQueue == nil {
		t.Error("Queues should be initialized")
	}

	if server.clientResults == nil || server.clientQueues == nil {
		t.Error("Maps should be initialized")
	}
}
