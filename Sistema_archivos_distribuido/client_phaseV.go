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
	// knownPeers contiene las direcciones de otros servidores de nombres
	knownPeers = []string{"localhost:8080", "localhost:8081"}
)

// reconnect intenta establecer una nueva conexión DTLS con un servidor conocido.
func reconnect() (*dtls.Conn, error) {
	certPEM, err := os.ReadFile("cert.pem")
	if err != nil {
		return nil, fmt.Errorf("no se puede leer el archivo de certificado: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		return nil, fmt.Errorf("falla al agregar certificado al pool")
	}

	dtlsConfig := &dtls.Config{
		InsecureSkipVerify:   false,
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}

	// Intenta conectar a todos los peers conocidos hasta que uno funcione
	for _, peerAddr := range knownPeers {
		addr, err := net.ResolveUDPAddr("udp", peerAddr)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al resolver dirección %s: %v", peerAddr, err))
			continue
		}

		logEvent("CLIENT", "RECONNECT_ATTEMPT", fmt.Sprintf("Intentando reconectar a %s...", peerAddr))
		conn, err := dtls.Dial("udp", addr, dtlsConfig)
		if err == nil {
			logEvent("CLIENT", "RECONNECT_SUCCESS", fmt.Sprintf("Reconexión exitosa a %s", peerAddr))
			return conn, nil
		}
		logEvent("CLIENT", "RECONNECT_FAILURE", fmt.Sprintf("Falla al reconectar a %s: %v", peerAddr, err))
	}

	return nil, fmt.Errorf("falla al conectar con cualquier servidor conocido")
}

func main() {
	conn, err := reconnect()
	if err != nil {
		logEvent("CLIENT", "CRITICAL_ERROR", fmt.Sprintf("No se pudo conectar a ningún servidor: %v", err))
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido")
	fmt.Println("Comandos: list, get <nombre_archivo>, exit")

	for {
		fmt.Print("-> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		var msg NetworkMessage
		msgBytes := []byte{}
		responseMsg := NetworkMessage{}
		readNeeded := true

		switch command {
		case "list":
			// Muestra el directorio local si existe, si no, lo solicita
			if len(localDirectory) > 0 {
				fmt.Println("\n--- Archivos Compartidos (Caché Local) ---")
				for name, entry := range localDirectory {
					fmt.Printf("- Nombre: %s, Tamaño: %d bytes, Dueño: %s\n", name, entry.Size, entry.OwnerIP)
				}
				fmt.Println("----------------------------------------\n")
				readNeeded = false
				continue // No se necesita leer del servidor
			} else {
				msg = NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
				logEvent("CLIENT", "REQUEST_SENT", "Solicitud de lista completa enviada.")
				msgBytes, _ = json.Marshal(msg)
			}
			
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

		case "exit":
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			return

		default:
			fmt.Println("Comando no reconocido. Comandos: list, get <nombre_archivo>, exit")
			readNeeded = false
			continue
		}

		if readNeeded {
			_, err = conn.Write(msgBytes)
			if err != nil {
				logEvent("CLIENT", "WRITE_ERROR", fmt.Sprintf("Falla al enviar mensaje: %v. Intentando reconectar...", err))
				conn, _ = reconnect()
				if conn == nil {
					logEvent("CLIENT", "FATAL", "Falla al reconectar. Cerrando cliente.")
					return
				}
				// Re-send the message after successful reconnection
				conn.Write(msgBytes)
			}
			
			buffer := make([]byte, 2048)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				logEvent("CLIENT", "TIMEOUT", "Error: El servidor no respondió a tiempo. Intentándolo de nuevo...")
				conn, _ = reconnect()
				if conn == nil {
					logEvent("CLIENT", "FATAL", "Falla al reconectar. Cerrando cliente.")
					return
				}
				continue
			}

			json.Unmarshal(buffer[:n], &responseMsg)
			
			if responseMsg.Type == "RESPONSE_LIST" {
				json.Unmarshal(responseMsg.Payload, &localDirectory)
				logEvent("CLIENT", "RESPONSE_LIST_RECEIVED", fmt.Sprintf("Lista de %d archivos recibida y guardada localmente.", len(localDirectory)))
				fmt.Println("\n--- Archivos Compartidos (Actualizados) ---")
				for name, entry := range localDirectory {
					fmt.Printf("- Nombre: %s, Tamaño: %d bytes, Dueño: %s\n", name, entry.Size, entry.OwnerIP)
				}
				fmt.Println("----------------------------------------\n")

			} else if responseMsg.Type == "RESPONSE" {
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
				fmt.Println("Archivo no encontrado o petición rechazada. Buscando en otros peers...")
				
				var fileName string
				json.Unmarshal(msg.Payload, &fileName)
				
				found := false
				for _, peerAddr := range knownPeers {
					if peerAddr == conn.RemoteAddr().String() {
						continue // Don't ask the same peer again
					}
					
					// Attempt to connect and request info from another peer
					newConn, err := reconnect()
					if err == nil {
						defer newConn.Close()
						
						conn.SetReadDeadline(time.Now().Add(10 * time.Second))
						
						msg := NetworkMessage{Type: "GET_FILE_INFO", Payload: msg.Payload}
						msgBytes, _ := json.Marshal(msg)
						newConn.Write(msgBytes)
						
						buffer := make([]byte, 2048)
						n, err := newConn.Read(buffer)
						if err == nil {
							var peerResponse NetworkMessage
							json.Unmarshal(buffer[:n], &peerResponse)
							if peerResponse.Type == "RESPONSE" {
								var entry DirectoryEntry
								json.Unmarshal(peerResponse.Payload, &entry)
								logEvent("CLIENT", "INFO_FOUND", fmt.Sprintf("Información del archivo '%s' encontrada en peer %s", fileName, peerAddr))
								fmt.Printf("Archivo encontrado. Dueño: %s\n", entry.OwnerIP)
								found = true
								break
							}
						}
					}
				}
				if !found {
					fmt.Println("No se pudo encontrar el archivo en ningún servidor conocido.")
				}
			}
		}
	}
}
