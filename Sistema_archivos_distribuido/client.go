package main

import (
	"bufio"
	"crypto/tls"
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
	knownServers   = []string{"localhost:8080"} // O la dirección de tu servidor.
	localDirectory = make(map[string]DirectoryEntry)
)

func getDTLSConfig() (*dtls.Config, error) {
	caCert, err := os.ReadFile("ca.crt")
	if err != nil {
		return nil, fmt.Errorf("falla al cargar certificado de la CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("falla al agregar certificado de la CA al pool")
	}

	cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		return nil, fmt.Errorf("falla al cargar el par de claves: %v", err)
	}

	return &dtls.Config{
		Certificates:         []tls.Certificate{cert},
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}, nil
}

func connectToServer() (*dtls.Conn, error) {
	dtlsConfig, err := getDTLSConfig()
	if err != nil {
		return nil, err
	}
	addr, err := net.ResolveUDPAddr("udp", knownServers[0])
	if err != nil {
		return nil, fmt.Errorf("falla al resolver dirección: %v", err)
	}
	conn, err := dtls.Dial("udp", addr, dtlsConfig)
	if err != nil {
		return nil, fmt.Errorf("falla al conectar: %v", err)
	}
	return conn, nil
}

func main() {
	conn, err := connectToServer()
	if err != nil {
		logEvent("CLIENT", "CRITICAL_ERROR", fmt.Sprintf("No se pudo conectar al servidor: %v", err))
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido - Modo CLI")
	fmt.Println("Comandos: list, get <nombre>, add <nombre>, edit <nombre>, exit")

	for {
		fmt.Print("-> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		var msg NetworkMessage
		var msgBytes []byte
		readNeeded := true
		
		conn.SetReadDeadline(time.Time{})

		switch command {
		case "list":
			msg = NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
			logEvent("CLIENT", "REQUEST_SENT", "Solicitud de lista completa enviada.")
			msgBytes, _ = json.Marshal(msg)

		case "get":
			if len(parts) < 2 {
				fmt.Println("Uso: get <nombre_archivo>")
				readNeeded = false
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
			logEvent("CLIENT", "REQUEST_SENT", fmt.Sprintf("Solicitud de info para '%s' enviada.", fileName))
			msgBytes, _ = json.Marshal(msg)

		case "add":
			if len(parts) < 2 {
				fmt.Println("Uso: add <nombre_archivo>")
				readNeeded = false
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "ADD_FILE", Payload: fileNameBytes}
			logEvent("CLIENT", "ADD_REQUEST", fmt.Sprintf("Solicitud para agregar archivo '%s' enviada.", fileName))
			msgBytes, _ = json.Marshal(msg)

		case "edit":
			if len(parts) < 2 {
				fmt.Println("Uso: edit <nombre_archivo>")
				readNeeded = false
				continue
			}
			fileName := parts[1]
			logEvent("CLIENT", "EDIT_REQUEST", fmt.Sprintf("Solicitud de edición para '%s'.", fileName))

			// Primero, obtener el contenido del archivo desde el servidor
			fileNameBytes, _ := json.Marshal(fileName)
			requestMsg := NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
			requestBytes, _ := json.Marshal(requestMsg)
			conn.Write(requestBytes)
			
			// Esperar la respuesta
			buffer := make([]byte, 4096)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				logEvent("CLIENT", "TIMEOUT", "Error: El servidor no respondió a tiempo. Verifique la conexión.")
				readNeeded = false
				continue
			}

			var responseMsg NetworkMessage
			json.Unmarshal(buffer[:n], &responseMsg)

			if responseMsg.Type == "FILE_RESPONSE" {
				// Guardar el archivo temporalmente
				tempFile := "edit_" + fileName
				err := os.WriteFile(tempFile, responseMsg.Payload, 0644)
				if err != nil {
					logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al guardar el archivo temporal: %v", err))
					readNeeded = false
					continue
				}

				fmt.Printf("Archivo descargado y guardado como '%s'.\n", tempFile)
				fmt.Printf("Por favor, edita el archivo y presiona Enter para subir los cambios.\n")
				bufio.NewReader(os.Stdin).ReadString('\n')

				// Leer el archivo modificado
				modifiedContent, err := os.ReadFile(tempFile)
				if err != nil {
					logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al leer el archivo modificado: %v", err))
					os.Remove(tempFile)
					readNeeded = false
					continue
				}

				// Obtener la información de versión y TTL de la caché local
				entry, found := localDirectory[fileName]
				if !found {
					logEvent("CLIENT", "ERROR", "No se encontró la entrada del archivo en la caché local para la versión.")
					os.Remove(tempFile)
					readNeeded = false
					continue
				}

				// Preparar el mensaje de actualización
				fileUpdate := FileUpdate{
					FileName:         fileName,
					Content:          modifiedContent,
					ModificationDate: time.Now(),
					Version:          entry.Version,
				}
				payloadBytes, _ := json.Marshal(fileUpdate)
				msg = NetworkMessage{Type: "FILE_WRITE_UPDATE", Payload: payloadBytes}
				msgBytes, _ = json.Marshal(msg)
				
				// Eliminar el archivo temporal
				os.Remove(tempFile)

			} else {
				fmt.Println("Error del servidor:", string(responseMsg.Payload))
				readNeeded = false
				continue
			}

		case "exit":
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			return

		default:
			fmt.Println("Comando no reconocido. Comandos: list, get <nombre>, add <nombre>, edit <nombre>, exit")
			readNeeded = false
			continue
		}

		if readNeeded {
			_, err = conn.Write(msgBytes)
			if err != nil {
				logEvent("CLIENT", "WRITE_ERROR", fmt.Sprintf("Falla al enviar mensaje: %v. Intentando reconectar...", err))
				newConn, reconnectErr := connectToServer()
				if reconnectErr != nil {
					logEvent("CLIENT", "FATAL", "Falla al reconectar. Cerrando cliente.")
					return
				}
				conn.Close()
				conn = newConn
				conn.Write(msgBytes)
			}

			buffer := make([]byte, 4096)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				logEvent("CLIENT", "TIMEOUT", "Error: El servidor no respondió a tiempo. Verifique la conexión.")
				continue
			}

			var responseMsg NetworkMessage
			json.Unmarshal(buffer[:n], &responseMsg)

			switch responseMsg.Type {
			case "RESPONSE_LIST":
				var directory map[string]DirectoryEntry
				json.Unmarshal(responseMsg.Payload, &directory)
				localDirectory = directory
				logEvent("CLIENT", "RESPONSE_LIST_RECEIVED", fmt.Sprintf("Lista de %d archivos recibida y guardada localmente.", len(localDirectory)))
				fmt.Println("\n--- Archivos Compartidos (Actualizados) ---")
				for name, entry := range localDirectory {
					fmt.Printf("- Nombre: %s, Tamaño: %d bytes, Dueño: %s, Versión: %d\n", name, entry.Size, entry.OwnerIP, entry.Version)
				}
				fmt.Println("----------------------------------------\n")

			case "RESPONSE":
				var entry DirectoryEntry
				json.Unmarshal(responseMsg.Payload, &entry)
				localDirectory[entry.FileName] = entry
				logEvent("CLIENT", "RESPONSE_RECEIVED", fmt.Sprintf("Información recibida para '%s'.", entry.FileName))
				fmt.Printf("\n--- Atributos del Archivo ---\n")
				fmt.Printf("Nombre: %s\n", entry.FileName)
				fmt.Printf("Tamaño: %d bytes\n", entry.Size)
				fmt.Printf("Fecha de Modificación: %s\n", entry.ModificationDate)
				fmt.Printf("Dueño: %s\n", entry.OwnerIP)
				fmt.Printf("Versión: %d\n", entry.Version)
				fmt.Println("---------------------------\n")

			case "UPDATE_ACK":
				fmt.Println("✅ Servidor:", string(responseMsg.Payload))

			case "UPDATE_REJECTED":
				fmt.Println("❌ Servidor:", string(responseMsg.Payload))

			case "NACK":
				fmt.Println("❌ Servidor:", string(responseMsg.Payload))

			default:
				logEvent("CLIENT", "UNEXPECTED_RESPONSE", fmt.Sprintf("Respuesta inesperada del servidor: %s", responseMsg.Type))
			}
		}
	}
}
