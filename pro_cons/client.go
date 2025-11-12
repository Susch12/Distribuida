package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	pb "calculator/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Aplica la función matemática a los 3 números
// Por defecto: suma de los 3 números
func applyMathFunction(num1, num2, num3 int32) int32 {
	// Función matemática: suma
	return num1 + num2 + num3
}

func main() {
	// Obtener el ID del cliente (por argumento o generar uno)
	var clientID string
	if len(os.Args) > 1 {
		clientID = os.Args[1]
	} else {
		clientID = fmt.Sprintf("Cliente-%d", time.Now().UnixNano()%10000)
	}

	// Conectar al servidor con opciones de rendimiento optimizadas
	conn, err := grpc.Dial("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*10), // 10 MB
			grpc.MaxCallSendMsgSize(1024*1024*10), // 10 MB
		),
		// Opciones de keep-alive para mantener conexiones activas
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second, // Enviar pings cada 10 segundos
			Timeout:             3 * time.Second,  // Esperar pong por 3 segundos
			PermitWithoutStream: true,             // Permitir pings sin streams activos
		}),
	)
	if err != nil {
		log.Fatalf("[%s] No se pudo conectar: %v", clientID, err)
	}
	defer conn.Close()

	client := pb.NewProducerConsumerClient(conn)

	log.Printf("[%s] Conectado al servidor, comenzando a procesar...", clientID)

	resultsProcessed := 0
	consecutiveFailures := 0
	maxConsecutiveFailures := 5

	// Reutilizar contextos para mejor rendimiento
	for {
		// Solicitar números al servidor (timeout reducido para mejor rendimiento)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		response, err := client.GetNumbers(ctx, &pb.NumberRequest{ClientId: clientID})
		cancel()

		if err != nil {
			log.Printf("[%s] Error al obtener números: %v", clientID, err)
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				log.Printf("[%s] Demasiados errores consecutivos, deteniendo cliente", clientID)
				break
			}
			time.Sleep(1 * time.Second)
			continue
		}

		// Resetear contador de fallos
		consecutiveFailures = 0

		// Verificar si el sistema se detuvo
		if response.SystemStopped {
			log.Printf("[%s] Sistema detenido por el servidor", clientID)
			break
		}

		// Verificar si hay números disponibles
		if !response.Available {
			// Cola vacía temporalmente, esperar un poco
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Aplicar la función matemática
		result := applyMathFunction(response.Num1, response.Num2, response.Num3)

		// Enviar el resultado al servidor (timeout reducido)
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		submitResp, err := client.SubmitResult(ctx, &pb.ResultRequest{
			ClientId: clientID,
			VectorId: response.VectorId,
			Result:   result,
		})
		cancel()

		if err != nil {
			log.Printf("[%s] Error al enviar resultado: %v", clientID, err)
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				log.Printf("[%s] Demasiados errores al enviar, deteniendo cliente", clientID)
				break
			}
			continue
		}

		resultsProcessed++

		// Mostrar progreso cada 1000 resultados
		if resultsProcessed%1000 == 0 {
			log.Printf("[%s] Procesados %d resultados (Total global: %d)",
				clientID, resultsProcessed, submitResp.TotalResults)
		}

		// Verificar si el sistema debe detenerse
		if submitResp.SystemStopped {
			log.Printf("[%s] Sistema alcanzó el límite, deteniendo...", clientID)
			break
		}
	}

	log.Printf("[%s] Finalizando. Total procesado por este cliente: %d resultados",
		clientID, resultsProcessed)
}
