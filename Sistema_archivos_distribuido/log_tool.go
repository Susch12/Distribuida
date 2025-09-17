package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// logEntry es la estructura de los logs que definimos.
type logEntry struct {
	Timestamp time.Time
	Module    string
	Action    string
	Details   string
}

func printHelpPanel() {
	fmt.Println("-------------------------------------------")
	fmt.Println("         Herramienta de Análisis de Logs         ")
	fmt.Println("-------------------------------------------")
	fmt.Println("Descripción:")
	fmt.Println("  Esta herramienta lee, combina y ordena cronológicamente los logs de")
	fmt.Println("  uno o más servidores y clientes para facilitar el seguimiento de operaciones.")
	fmt.Println("\nUso:")
	fmt.Println("  go run log_tool.go [opciones] <archivo_log_1> <archivo_log_2> ...")
	fmt.Println("\nOpciones:")
	fmt.Println("  --file=<nombre_archivo>    Filtra los logs para mostrar solo los eventos")
	fmt.Println("                             relacionados con un archivo específico.")
	fmt.Println("\nEjemplos:")
	fmt.Println("  1. Ver todos los logs de un servidor:")
	fmt.Println("     go run log_tool.go server.log")
	fmt.Println("\n  2. Combinar logs de varios servidores y clientes:")
	fmt.Println("     go run log_tool.go server1.log server2.log client.log")
	fmt.Println("\n  3. Seguir la traza de una operación sobre un archivo en específico:")
	fmt.Println("     go run log_tool.go --file=perpetual_file.doc server1.log server2.log")
	fmt.Println("-------------------------------------------")
}

func main() {
	var fileFilter string
	flag.StringVar(&fileFilter, "file", "", "Filtrar por nombre de archivo específico")
	flag.Parse()

	if len(flag.Args()) < 1 {
		printHelpPanel()
		return
	}

	var allLogs []logEntry

	for _, filename := range flag.Args() {
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
				// Ignorar líneas malformadas.
				continue
			}
			allLogs = append(allLogs, entry)
		}
	}

	// Ordenar los logs por su marca de tiempo.
	for i := 0; i < len(allLogs); i++ {
		for j := i + 1; j < len(allLogs); j++ {
			if allLogs[i].Timestamp.After(allLogs[j].Timestamp) {
				allLogs[i], allLogs[j] = allLogs[j], allLogs[i]
			}
		}
	}

	fmt.Println("--- Trazas de Operaciones (Ordenadas Cronológicamente) ---")
	for _, entry := range allLogs {
		if fileFilter != "" && !strings.Contains(entry.Details, fileFilter) && !strings.Contains(entry.Action, fileFilter) {
			continue
		}
		fmt.Printf("[%s] %s: %s - %s\n", entry.Module, entry.Timestamp.Format("15:04:05.000"), entry.Action, entry.Details)
	}
	fmt.Println("---------------------------------------------------------")
}
