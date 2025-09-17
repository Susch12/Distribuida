package main

import (
	"bufio"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/pion/dtls/v2"
)

// Definiciones de structs (logEntry, NetworkMessage, DirectoryEntry)
// Se asume que structs.go y logEvent() están en el mismo paquete.

type logEntry struct {
	Timestamp time.Time
	Module    string
	Action    string
	Details   string
}

func logEvent(module, action, details string) {
	entry := logEntry{
		Timestamp: time.Now(),
		Module:    module,
		Action:    action,
		Details:   details,
	}
	logBytes, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Error al serializar log: %v", err)
		return
	}
	fmt.Printf("[%s] [%s] %s: %s\n", entry.Module, entry.Action, entry.Timestamp.Format("15:04:05"), entry.Details)
	file, err := os.OpenFile("client.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error al abrir el archivo de log: %v", err)
		return
	}
	defer file.Close()
	file.Write(logBytes)
	file.WriteString("\n")
}

var (
	// localDirectory es la copia local del directorio del servidor.
	localDirectory = make(map[string]DirectoryEntry)
)

func main() {
	certPEM, err := os.ReadFile("cert.pem")
	if err != nil {
		logEvent("CLIENT", "ERROR", "No se puede leer el archivo de certificado. ¿Está el servidor corriendo?")
		return
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		logEvent("CLIENT", "ERROR", "Falla al agregar certificado al pool")
		panic("Falla al agregar certificado al pool")
	}

	dtlsConfig := &dtls.Config{
		InsecureSkipVerify:   false,
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}
	addr, err := net.ResolveUDPAddr("udp", "localhost:8080")
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al resolver dirección: %v", err))
		panic(err)
	}

	logEvent("CLIENT", "HANDSHAKE_STARTED", "Iniciando handshake DTLS...")
	conn, err := dtls.Dial("udp", addr, dtlsConfig)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla en el handshake DTLS: %v", err))
		panic(err)
	}
	defer conn.Close()
	logEvent("CLIENT", "HANDSHAKE_COMPLETED", "Handshake DTLS completado y certificado validado.")

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido")
	fmt.Println("Comandos: list, get <nombre_archivo>, exit")

	for {
		fmt.Print("-> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		switch command {
		case "list":
			// Si el directorio local está vacío, solicita la lista completa al servidor.
			if len(localDirectory) == 0 {
				msg := NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
				logEvent("CLIENT", "REQUEST_SENT", "Solicitud de lista completa enviada.")
				msgBytes, _ := json.Marshal(msg)
				conn.Write(msgBytes)

				buffer := make([]byte, 2048)
				// Establecer un timeout de 10 segundos para la lectura.
				conn.SetReadDeadline(time.Now().Add(10 * time.Second))
				n, err := conn.Read(buffer)
				if err != nil {
					logEvent("CLIENT", "TIMEOUT", "Error: El servidor no respondió a tiempo. Inténtelo de nuevo más tarde.")
					continue
				}

				var responseMsg NetworkMessage
				json.Unmarshal(buffer[:n], &responseMsg)
				
				if responseMsg.Type == "RESPONSE_LIST" {
					json.Unmarshal(responseMsg.Payload, &localDirectory)
					logEvent("CLIENT", "RESPONSE_LIST_RECEIVED", fmt.Sprintf("Lista de %d archivos recibida y guardada localmente.", len(localDirectory)))
				} else {
					logEvent("CLIENT", "NACK_RECEIVED", "El servidor no pudo procesar la solicitud de lista.")
				}
			}
			
			// Muestra siempre el contenido del directorio local.
			fmt.Println("\n--- Archivos Compartidos ---")
			if len(localDirectory) == 0 {
				fmt.Println("El directorio está vacío o no se pudo obtener.")
			} else {
				for name, entry := range localDirectory {
					fmt.Printf("- Nombre: %s, Tamaño: %d bytes, Dueño: %s\n", name, entry.Size, entry.OwnerIP)
				}
			}
			fmt.Println("---------------------------\n")

		case "get":
			if len(parts) < 2 {
				fmt.Println("Uso: get <nombre_archivo>")
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg := NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
			
			logEvent("CLIENT", "REQUEST_SENT", fmt.Sprintf("Solicitud de info para '%s' enviada.", fileName))
			msgBytes, _ := json.Marshal(msg)
			conn.Write(msgBytes)

			buffer := make([]byte, 2048)
			// Establecer un timeout de 10 segundos para la lectura.
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				logEvent("CLIENT", "TIMEOUT", "Error: El servidor no respondió a tiempo. Inténtelo de nuevo más tarde.")
				continue
			}
			
			var responseMsg NetworkMessage
			json.Unmarshal(buffer[:n], &responseMsg)

			if responseMsg.Type == "RESPONSE" {
				var entry DirectoryEntry
				json.Unmarshal(responseMsg.Payload, &entry)
				logEvent("CLIENT", "RESPONSE_RECEIVED", fmt.Sprintf("Información recibida para '%s'. (Autoritativa: %t)", entry.FileName, responseMsg.Authoritative))
				fmt.Printf("\n--- Atributos del Archivo ---\n")
				fmt.Printf("Nombre: %s\n", entry.FileName)
				fmt.Printf("Tamaño: %d bytes\n", entry.Size)
				fmt.Printf("Fecha de Modificación: %s\n", entry.ModificationDate)
				fmt.Printf("Dueño: %s\n", entry.OwnerIP)
				fmt.Println("---------------------------\n")

			} else if responseMsg.Type == "NACK" {
				logEvent("CLIENT", "NACK_RECEIVED", fmt.Sprintf("Petición rechazada: %s", string(responseMsg.Payload)))
				fmt.Println("Archivo no encontrado o petición rechazada.")
			}
		case "exit":
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			return
		default:
			fmt.Println("Comando no reconocido. Comandos: list, get <nombre_archivo>, exit")
		}
	}
}
