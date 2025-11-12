package main

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "calculator/proto"
)

// TestConcurrentSubmitResult verifica que no haya race conditions
// cuando múltiples clientes envían resultados simultáneamente
func TestConcurrentSubmitResult(t *testing.T) {
	server := newProducerConsumerServer()

	// Número de goroutines concurrentes
	numClients := 100
	resultsPerClient := 1000

	var wg sync.WaitGroup
	wg.Add(numClients)

	// Simular múltiples clientes enviando resultados
	for i := 0; i < numClients; i++ {
		go func(clientNum int) {
			defer wg.Done()

			for j := 0; j < resultsPerClient; j++ {
				ctx := context.Background()
				_, err := server.SubmitResult(ctx, &pb.ResultRequest{
					ClientId: "test-client",
					VectorId: "1-2-3",
					Result:   6,
				})

				if err != nil {
					t.Errorf("Error al enviar resultado: %v", err)
				}
			}
		}(i)
	}

	// Esperar a que todas las goroutines terminen
	wg.Wait()

	// Verificar que el contador total sea correcto
	server.statsMutex.RLock()
	expectedTotal := int64(numClients * resultsPerClient)
	if server.totalResults != expectedTotal {
		t.Errorf("Expected %d results, got %d", expectedTotal, server.totalResults)
	}
	server.statsMutex.RUnlock()
}

// TestProducerVectorUniqueness verifica que el productor
// genere vectores únicos sin race conditions
func TestProducerVectorUniqueness(t *testing.T) {
	server := newProducerConsumerServer()

	// Iniciar el productor
	go server.produceVectors()

	// Consumir vectores y verificar unicidad
	seen := make(map[string]bool)
	duplicates := 0

	timeout := time.After(2 * time.Second)
	for i := 0; i < 1000; i++ {
		select {
		case vector := <-server.queue:
			if seen[vector.ID] {
				duplicates++
			}
			seen[vector.ID] = true
		case <-timeout:
			// Timeout después de 2 segundos
			goto done
		}
	}

done:
	// Detener el productor
	server.stopMutex.Lock()
	server.systemStopped = true
	server.stopMutex.Unlock()

	// Esperar a que el productor cierre la cola
	time.Sleep(100 * time.Millisecond)

	if duplicates > 0 {
		t.Errorf("Found %d duplicate vectors", duplicates)
	}

	if len(seen) == 0 {
		t.Error("No vectors were produced")
	}
}

// TestQueueClosedAccess verifica que no haya race conditions
// al acceder a queueClosed desde múltiples goroutines
func TestQueueClosedAccess(t *testing.T) {
	server := newProducerConsumerServer()

	var wg sync.WaitGroup
	numReaders := 50

	wg.Add(numReaders)

	// Múltiples lectores intentan leer queueClosed
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				server.queueMutex.RLock()
				_ = server.queueClosed
				server.queueMutex.RUnlock()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// Un escritor modifica queueClosed
	go func() {
		time.Sleep(50 * time.Millisecond)
		server.queueMutex.Lock()
		server.queueClosed = true
		server.queueMutex.Unlock()
	}()

	wg.Wait()
}

// TestGetNumbersConcurrent verifica que GetNumbers maneje
// correctamente múltiples solicitudes concurrentes
func TestGetNumbersConcurrent(t *testing.T) {
	server := newProducerConsumerServer()

	// Iniciar el productor
	go server.produceVectors()

	var wg sync.WaitGroup
	numClients := 50
	wg.Add(numClients)

	errors := make(chan error, numClients)

	// Múltiples clientes solicitan números
	for i := 0; i < numClients; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := server.GetNumbers(ctx, &pb.NumberRequest{
				ClientId: "test-client",
			})

			if err != nil {
				errors <- err
				return
			}

			if resp.Available && (resp.Num1 < MIN_NUMBER || resp.Num1 > MAX_NUMBER) {
				t.Errorf("Number out of range: %d", resp.Num1)
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Verificar que no hubo errores
	for err := range errors {
		t.Errorf("Error in GetNumbers: %v", err)
	}

	// Detener el productor
	server.stopMutex.Lock()
	server.systemStopped = true
	server.stopMutex.Unlock()
}
