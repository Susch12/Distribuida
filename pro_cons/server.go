package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	pb "calculator/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	MAX_RESULTS       = 1000000 // Un millón de resultados
	MIN_NUMBER        = 1
	MAX_NUMBER        = 1000
	QUEUE_BUFFER_SIZE = 10000 // Tamaño inicial de la cola
)

// Vector representa un conjunto único de 3 números
type Vector struct {
	ID   string
	Num1 int32
	Num2 int32
	Num3 int32
}

type producerConsumerServer struct {
	pb.UnimplementedProducerConsumerServer

	// Cola de vectores
	queue       chan Vector
	queueClosed bool
	queueMutex  sync.RWMutex // Protege queueClosed

	// Control de vectores únicos
	generatedVectors map[string]bool
	vectorMutex      sync.Mutex

	// Contadores y estadísticas
	totalResults    int64
	resultSum       int64
	clientStats     map[string]int64 // Contador de resultados por cliente
	statsMutex      sync.RWMutex

	// Control del sistema
	systemStopped bool
	stopMutex     sync.RWMutex

	// Canal para señalar cuando se alcanza el límite
	stopChan chan bool
}

func newProducerConsumerServer() *producerConsumerServer {
	return &producerConsumerServer{
		queue:            make(chan Vector, QUEUE_BUFFER_SIZE),
		generatedVectors: make(map[string]bool),
		clientStats:      make(map[string]int64),
		stopChan:         make(chan bool, 1),
	}
}

// Genera un ID único para el vector basado en sus números
func vectorID(n1, n2, n3 int32) string {
	return fmt.Sprintf("%d-%d-%d", n1, n2, n3)
}

// Genera vectores únicos de 3 números aleatorios y los almacena en la cola
func (s *producerConsumerServer) produceVectors() {
	// Nota: rand.Seed() ya no es necesario en Go 1.20+
	// Go usa un generador de números aleatorios seguro por defecto
	vectorCount := 0

	log.Println("Productor: Iniciando generación de vectores...")

	for {
		// Verificar si el sistema debe detenerse
		s.stopMutex.RLock()
		stopped := s.systemStopped
		s.stopMutex.RUnlock()

		if stopped {
			log.Println("Productor: Sistema detenido, cerrando cola...")
			s.queueMutex.Lock()
			close(s.queue)
			s.queueClosed = true
			s.queueMutex.Unlock()
			return
		}

		// Generar 3 números aleatorios
		num1 := int32(rand.Intn(MAX_NUMBER-MIN_NUMBER+1) + MIN_NUMBER)
		num2 := int32(rand.Intn(MAX_NUMBER-MIN_NUMBER+1) + MIN_NUMBER)
		num3 := int32(rand.Intn(MAX_NUMBER-MIN_NUMBER+1) + MIN_NUMBER)

		id := vectorID(num1, num2, num3)

		// Verificar si el vector es único
		s.vectorMutex.Lock()
		if s.generatedVectors[id] {
			s.vectorMutex.Unlock()
			continue // Ya existe, generar otro
		}
		s.generatedVectors[id] = true
		s.vectorMutex.Unlock()

		// Crear el vector
		vector := Vector{
			ID:   id,
			Num1: num1,
			Num2: num2,
			Num3: num3,
		}

		// Intentar agregar a la cola (con timeout para evitar bloqueo)
		select {
		case s.queue <- vector:
			vectorCount++
			if vectorCount%10000 == 0 {
				log.Printf("Productor: %d vectores únicos generados", vectorCount)
			}
		case <-time.After(100 * time.Millisecond):
			// Cola llena, reintentar
		}
	}
}

// GetNumbers - Cliente solicita números de la cola
func (s *producerConsumerServer) GetNumbers(ctx context.Context, req *pb.NumberRequest) (*pb.NumberResponse, error) {
	// Verificar si el sistema está detenido
	s.stopMutex.RLock()
	stopped := s.systemStopped
	s.stopMutex.RUnlock()

	if stopped {
		return &pb.NumberResponse{
			Available:      false,
			SystemStopped:  true,
		}, nil
	}

	// Intentar obtener un vector de la cola
	select {
	case vector, ok := <-s.queue:
		if !ok {
			// Cola cerrada
			return &pb.NumberResponse{
				Available:     false,
				SystemStopped: true,
			}, nil
		}

		return &pb.NumberResponse{
			Available:     true,
			VectorId:      vector.ID,
			Num1:          vector.Num1,
			Num2:          vector.Num2,
			Num3:          vector.Num3,
			SystemStopped: false,
		}, nil

	case <-time.After(100 * time.Millisecond):
		// Timeout - cola vacía temporalmente (reducido para mejor rendimiento)
		return &pb.NumberResponse{
			Available:     false,
			SystemStopped: false,
		}, nil
	}
}

// SubmitResult - Cliente envía resultado procesado
func (s *producerConsumerServer) SubmitResult(ctx context.Context, req *pb.ResultRequest) (*pb.ResultResponse, error) {
	// Verificar si el sistema ya está detenido
	s.stopMutex.RLock()
	stopped := s.systemStopped
	s.stopMutex.RUnlock()

	if stopped {
		return &pb.ResultResponse{
			Accepted:      false,
			TotalResults:  s.totalResults,
			SystemStopped: true,
		}, nil
	}

	// Actualizar estadísticas (mantener el lock lo más corto posible)
	s.statsMutex.Lock()
	s.totalResults++
	s.resultSum += int64(req.Result)
	s.clientStats[req.ClientId]++
	currentTotal := s.totalResults
	shouldLog := (currentTotal % 10000 == 0)
	s.statsMutex.Unlock()

	// Logging fuera del mutex crítico para mejor rendimiento
	if shouldLog {
		log.Printf("Progreso: %d resultados recibidos (%.2f%% completado)",
			currentTotal, float64(currentTotal)/float64(MAX_RESULTS)*100)
	}

	// Verificar si alcanzamos el límite
	if currentTotal >= MAX_RESULTS {
		s.stopMutex.Lock()
		if !s.systemStopped {
			s.systemStopped = true
			s.stopChan <- true
			log.Println("¡Límite de 1 millón de resultados alcanzado!")
		}
		s.stopMutex.Unlock()

		return &pb.ResultResponse{
			Accepted:      true,
			TotalResults:  currentTotal,
			SystemStopped: true,
		}, nil
	}

	return &pb.ResultResponse{
		Accepted:      true,
		TotalResults:  currentTotal,
		SystemStopped: false,
	}, nil
}

// Muestra las estadísticas finales del sistema
func (s *producerConsumerServer) showFinalStats() {
	s.statsMutex.RLock()
	defer s.statsMutex.RUnlock()

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("ESTADÍSTICAS FINALES DEL SISTEMA")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Total de resultados recibidos: %d\n", s.totalResults)
	fmt.Printf("Suma total de todos los resultados: %d\n", s.resultSum)
	fmt.Printf("Vectores únicos generados: %d\n", len(s.generatedVectors))
	fmt.Println()

	fmt.Println("RANKING DE CLIENTES (por número de resultados resueltos):")
	fmt.Println(strings.Repeat("-", 70))

	// Crear ranking de clientes
	type clientRank struct {
		id    string
		count int64
	}

	var ranking []clientRank
	for clientID, count := range s.clientStats {
		ranking = append(ranking, clientRank{id: clientID, count: count})
	}

	// Ordenar por cantidad (burbuja simple para pocos clientes)
	for i := 0; i < len(ranking); i++ {
		for j := i + 1; j < len(ranking); j++ {
			if ranking[j].count > ranking[i].count {
				ranking[i], ranking[j] = ranking[j], ranking[i]
			}
		}
	}

	// Mostrar ranking
	for i, client := range ranking {
		percentage := float64(client.count) / float64(s.totalResults) * 100
		fmt.Printf("%2d. Cliente %-20s: %8d resultados (%.2f%%)\n",
			i+1, client.id, client.count, percentage)
	}

	fmt.Println(strings.Repeat("=", 70))
}

func main() {
	// Crear el servidor
	server := newProducerConsumerServer()

	// Iniciar el productor de vectores en segundo plano
	go server.produceVectors()

	// Configurar el servidor gRPC
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Error al escuchar: %v", err)
	}

	// Crear servidor gRPC con opciones de rendimiento optimizadas
	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(1000),              // Permitir más streams concurrentes
		grpc.MaxRecvMsgSize(1024*1024*10),            // 10 MB max receive
		grpc.MaxSendMsgSize(1024*1024*10),            // 10 MB max send
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute, // Cerrar conexiones inactivas después de 15 min
			MaxConnectionAge:      30 * time.Minute, // Máxima edad de conexión
			MaxConnectionAgeGrace: 5 * time.Second,  // Tiempo de gracia para cerrar
			Time:                  5 * time.Second,  // Ping cada 5 segundos
			Timeout:               1 * time.Second,  // Timeout de ping
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second, // Mínimo tiempo entre pings del cliente
			PermitWithoutStream: true,            // Permitir pings sin streams
		}),
	)
	pb.RegisterProducerConsumerServer(grpcServer, server)

	log.Println(strings.Repeat("=", 70))
	log.Println("Servidor gRPC Productor-Consumidor iniciado en puerto 50051")
	log.Printf("Generando vectores únicos de 3 números (%d-%d)...", MIN_NUMBER, MAX_NUMBER)
	log.Printf("Sistema se detendrá después de %d resultados", MAX_RESULTS)
	log.Println(strings.Repeat("=", 70))

	// Servir en segundo plano
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Error al servir: %v", err)
		}
	}()

	// Esperar señal de parada
	<-server.stopChan

	// Dar tiempo para que los últimos clientes terminen
	time.Sleep(2 * time.Second)

	// Detener el servidor
	log.Println("Deteniendo el servidor...")
	grpcServer.GracefulStop()

	// Mostrar estadísticas finales
	server.showFinalStats()
}
