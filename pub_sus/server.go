package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"pubsub/proto"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
)

type SelectionCriteria string

const (
	RandomCriteria      SelectionCriteria = "aleatorio"
	WeightedCriteria    SelectionCriteria = "ponderado"
	ConditionalCriteria SelectionCriteria = "condicional"
)

type QueueType string

const (
	PrimaryQueue   QueueType = "primary"
	SecondaryQueue QueueType = "secondary"
	TertiaryQueue  QueueType = "tertiary"
)

type PubSubServer struct {
	proto.UnimplementedPubSubServiceServer

	// Message queues
	primaryQueue   chan *proto.NumberSet
	secondaryQueue chan *proto.NumberSet
	tertiaryQueue  chan *proto.NumberSet

	// Server state
	criteria       SelectionCriteria
	messageID      int
	messageIDMu    sync.Mutex

	// Results tracking
	results        []int
	resultsMu      sync.Mutex
	clientResults  map[int32][]int // clientID -> list of results
	clientQueues   map[int32][]string // clientID -> subscribed queues

	// Control
	stopPublishing bool
	stopMu         sync.Mutex
}

func NewPubSubServer(criteria SelectionCriteria) *PubSubServer {
	return &PubSubServer{
		primaryQueue:   make(chan *proto.NumberSet, 1000),
		secondaryQueue: make(chan *proto.NumberSet, 1000),
		tertiaryQueue:  make(chan *proto.NumberSet, 1000),
		criteria:       criteria,
		messageID:      0,
		results:        make([]int, 0),
		clientResults:  make(map[int32][]int),
		clientQueues:   make(map[int32][]string),
		stopPublishing: false,
	}
}

// Subscribe implements streaming RPC for clients to receive number sets
func (s *PubSubServer) Subscribe(req *proto.SubscribeRequest, stream proto.PubSubService_SubscribeServer) error {
	clientID := req.ClientId
	queues := req.Queues

	log.Printf("Client %d subscribed to queues: %v", clientID, queues)

	// Register client
	s.resultsMu.Lock()
	s.clientQueues[clientID] = queues
	if _, exists := s.clientResults[clientID]; !exists {
		s.clientResults[clientID] = make([]int, 0)
	}
	s.resultsMu.Unlock()

	// Create channels to listen to requested queues
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Client %d disconnected", clientID)
			return ctx.Err()
		default:
			// Client subscribes to one or two queues
			// If two queues, randomly select one message from either queue
			var msg *proto.NumberSet

			if len(queues) == 1 {
				// Single queue subscription
				msg = s.receiveFromQueue(queues[0])
			} else if len(queues) == 2 {
				// Dual queue subscription - randomly select
				if rand.Intn(2) == 0 {
					msg = s.receiveFromQueue(queues[0])
				} else {
					msg = s.receiveFromQueue(queues[1])
				}
			}

			if msg != nil {
				if err := stream.Send(msg); err != nil {
					log.Printf("Error sending to client %d: %v", clientID, err)
					return err
				}
			}
		}
	}
}

// receiveFromQueue tries to receive a message from the specified queue
func (s *PubSubServer) receiveFromQueue(queueName string) *proto.NumberSet {
	var queueChan chan *proto.NumberSet

	switch queueName {
	case "primary":
		queueChan = s.primaryQueue
	case "secondary":
		queueChan = s.secondaryQueue
	case "tertiary":
		queueChan = s.tertiaryQueue
	default:
		return nil
	}

	select {
	case msg := <-queueChan:
		return msg
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// SendResult receives results from clients
func (s *PubSubServer) SendResult(ctx context.Context, req *proto.ResultRequest) (*proto.ResultResponse, error) {
	s.resultsMu.Lock()
	s.results = append(s.results, int(req.Result))
	s.clientResults[req.ClientId] = append(s.clientResults[req.ClientId], int(req.Result))
	totalResults := len(s.results)
	s.resultsMu.Unlock()

	log.Printf("Received result from client %d: %d (Total results: %d)", req.ClientId, req.Result, totalResults)

	// Check if we've reached 1 million results (with proper locking)
	if totalResults >= 1000000 {
		s.stopMu.Lock()
		if !s.stopPublishing {
			s.stopPublishing = true
			s.stopMu.Unlock()
			s.printFinalReport()
		} else {
			s.stopMu.Unlock()
		}
	}

	return &proto.ResultResponse{Success: true}, nil
}

// GetStats returns current server statistics
func (s *PubSubServer) GetStats(ctx context.Context, req *proto.StatsRequest) (*proto.StatsResponse, error) {
	s.resultsMu.Lock()
	defer s.resultsMu.Unlock()

	totalSum := 0
	for _, result := range s.results {
		totalSum += result
	}

	clients := make([]*proto.ClientStats, 0)
	for clientID, queues := range s.clientQueues {
		clients = append(clients, &proto.ClientStats{
			ClientId:          clientID,
			SubscribedQueues:  queues,
			ResultsCount:      int32(len(s.clientResults[clientID])),
		})
	}

	return &proto.StatsResponse{
		TotalResults: int32(len(s.results)),
		TotalSum:     int32(totalSum),
		Clients:      clients,
	}, nil
}

// printFinalReport prints the final statistics
func (s *PubSubServer) printFinalReport() {
	fmt.Println("\n=== FINAL REPORT ===")

	totalSum := 0
	for _, result := range s.results {
		totalSum += result
	}

	fmt.Printf("Total de resultados: %d\n", len(s.results))
	fmt.Printf("Suma total de resultados: %d\n", totalSum)
	fmt.Println("\nClientes que trabajaron:")

	for clientID, queues := range s.clientQueues {
		resultsCount := len(s.clientResults[clientID])
		fmt.Printf("  - Cliente %d: %d resultados, suscrito a colas: %v\n", clientID, resultsCount, queues)
	}

	fmt.Println("====================")
}

// selectQueue determines which queue a message should go to based on criteria
func (s *PubSubServer) selectQueue(numbers []int) QueueType {
	switch s.criteria {
	case RandomCriteria:
		return s.selectRandomQueue()
	case WeightedCriteria:
		return s.selectWeightedQueue()
	case ConditionalCriteria:
		return s.selectConditionalQueue(numbers)
	default:
		return PrimaryQueue
	}
}

// selectRandomQueue: 33% chance for each queue
func (s *PubSubServer) selectRandomQueue() QueueType {
	r := rand.Intn(3)
	switch r {
	case 0:
		return PrimaryQueue
	case 1:
		return SecondaryQueue
	default:
		return TertiaryQueue
	}
}

// selectWeightedQueue: 50% primary, 30% secondary, 20% tertiary
func (s *PubSubServer) selectWeightedQueue() QueueType {
	r := rand.Intn(100)
	if r < 50 {
		return PrimaryQueue
	} else if r < 80 {
		return SecondaryQueue
	} else {
		return TertiaryQueue
	}
}

// selectConditionalQueue: based on even/odd count
func (s *PubSubServer) selectConditionalQueue(numbers []int) QueueType {
	evenCount := 0
	oddCount := 0

	for _, num := range numbers {
		if num%2 == 0 {
			evenCount++
		} else {
			oddCount++
		}
	}

	// Two even numbers -> primary
	if evenCount == 2 {
		return PrimaryQueue
	}
	// Two odd numbers -> secondary
	if oddCount == 2 {
		return SecondaryQueue
	}
	// Three even or three odd -> tertiary
	if evenCount == 3 || oddCount == 3 {
		return TertiaryQueue
	}

	// Default case (shouldn't happen with 2-3 numbers)
	return PrimaryQueue
}

// publishNumbers continuously generates and publishes number sets
func (s *PubSubServer) publishNumbers() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		s.stopMu.Lock()
		shouldStop := s.stopPublishing
		s.stopMu.Unlock()

		if shouldStop {
			log.Println("Reached 1 million results. Stopping number generation.")
			break
		}

		// Generate random number set (2 or 3 numbers)
		count := 2 + rand.Intn(2) // 2 or 3 numbers
		numbers := make([]int32, count)
		for i := 0; i < count; i++ {
			numbers[i] = int32(rand.Intn(100))
		}

		s.messageIDMu.Lock()
		s.messageID++
		msgID := s.messageID
		s.messageIDMu.Unlock()

		// Convert to int slice for queue selection
		intNumbers := make([]int, len(numbers))
		for i, n := range numbers {
			intNumbers[i] = int(n)
		}

		queue := s.selectQueue(intNumbers)

		msg := &proto.NumberSet{
			MessageId: int32(msgID),
			Numbers:   numbers,
			Queue:     string(queue),
		}

		// Send to appropriate queue
		switch queue {
		case PrimaryQueue:
			select {
			case s.primaryQueue <- msg:
			default:
				log.Println("Primary queue full, dropping message")
			}
		case SecondaryQueue:
			select {
			case s.secondaryQueue <- msg:
			default:
				log.Println("Secondary queue full, dropping message")
			}
		case TertiaryQueue:
			select {
			case s.tertiaryQueue <- msg:
			default:
				log.Println("Tertiary queue full, dropping message")
			}
		}
	}
}

func main() {
	port := flag.Int("port", 50051, "Server port")
	criteriaStr := flag.String("criteria", "aleatorio", "Selection criteria: aleatorio, ponderado, condicional")
	flag.Parse()

	criteria := SelectionCriteria(*criteriaStr)
	if criteria != RandomCriteria && criteria != WeightedCriteria && criteria != ConditionalCriteria {
		log.Fatalf("Invalid criteria: %s. Use: aleatorio, ponderado, or condicional", *criteriaStr)
	}

	log.Printf("Starting server on port %d with criteria: %s", *port, criteria)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := NewPubSubServer(criteria)

	// Start publishing numbers
	go server.publishNumbers()

	grpcServer := grpc.NewServer()
	proto.RegisterPubSubServiceServer(grpcServer, server)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		log.Printf("Server listening at %v", lis.Addr())
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, initiating graceful shutdown...", sig)

	// Stop publishing
	server.stopMu.Lock()
	server.stopPublishing = true
	server.stopMu.Unlock()

	// Print final report
	server.printFinalReport()

	// Gracefully stop the gRPC server
	log.Println("Stopping gRPC server...")
	grpcServer.GracefulStop()
	log.Println("Server shutdown complete")
}
