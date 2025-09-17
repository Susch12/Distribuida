package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// logEntry es la estructura de los logs que definimos en la fase 1.
type logEntry struct {
	Timestamp time.Time
	Module    string
	Action    string
	Details   string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Uso: go run log_tool.go <archivo_log_1> <archivo_log_2> ...")
		return
	}

	var allLogs []logEntry

	// Leer todos los archivos de log proporcionados
	for _, filename := range os.Args[1:] {
		file, err := os.Open(filename)
		if err != nil {
			fmt.Printf("Error al abrir el archivo %s: %v\n", filename, err)
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var entry logEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				fmt.Printf("Error al decodificar JSON en %s: %v\n", filename, err)
				continue
			}
			allLogs = append(allLogs, entry)
		}
	}

	// Ordenar los logs por su marca de tiempo
	// Nota: Para simplificar, usamos un algoritmo de ordenación básico.
	for i := 0; i < len(allLogs); i++ {
		for j := i + 1; j < len(allLogs); j++ {
			if allLogs[i].Timestamp.After(allLogs[j].Timestamp) {
				allLogs[i], allLogs[j] = allLogs[j], allLogs[i]
			}
		}
	}

	// Imprimir los logs ordenados
	fmt.Println("--- Trazas de Operaciones (Ordenadas Cronológicamente) ---")
	for _, entry := range allLogs {
		fmt.Printf("[%s] %s: %s - %s\n", entry.Module, entry.Timestamp.Format("15:04:05.000"), entry.Action, entry.Details)
	}
	fmt.Println("---------------------------------------------------------")
}
