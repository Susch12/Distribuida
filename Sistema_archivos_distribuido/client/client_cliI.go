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
	"sync"
	"time"

	"github.com/pion/dtls/v2"
)

// Las estructuras DirectoryEntry, NetworkMessage y FileUpdate se asumen que están
// en el archivo structs.go en el mismo paquete.

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
	knownServers   = []string{"localhost:8080", "localhost:8081"}
	localDirectory = make(map[string]DirectoryEntry)
	mu             sync.Mutex // Mutex para proteger el acceso a localDirectory
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

	cert, err := tls.LoadX509KeyPair("client.crt", "client.key")
	if err != nil {
		return nil, fmt.Errorf("falla al cargar el par de claves: %v", err)
	}

	return &dtls.Config{
		Certificates:         []tls.Certificate{cert},
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}, nil
}

func connectToPeer() (*dtls.Conn, error) {
	dtlsConfig, err := getDTLSConfig()
	if err != nil {
		return nil, err
	}
	for _, addr := range knownServers {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			logEvent("CLIENT", "CONNECTION_ATTEMPT", fmt.Sprintf("Falla al resolver dirección %s: %v", addr, err))
			continue
		}
		conn, err := dtls.Dial("udp", udpAddr, dtlsConfig)
		if err == nil {
			logEvent("CLIENT", "CONNECTION_SUCCESS", fmt.Sprintf("Conectado con éxito a: %s", addr))
			return conn, nil
		}
		logEvent("CLIENT", "CONNECTION_FAILURE", fmt.Sprintf("Falla al conectar con %s: %v", addr, err))
	}
	return nil, fmt.Errorf("no se pudo conectar a ningún peer conocido")
}

func networkHandler(requests <-chan NetworkMessage, responses chan<- NetworkMessage) {
	defer close(responses)
	var conn *dtls.Conn
	var err error

	// Bucle para mantener la conexión activa
	for {
		if conn == nil {
			conn, err = connectToPeer()
			if err != nil {
				logEvent("NETWORK_HANDLER", "CRITICAL_ERROR", fmt.Sprintf("No se pudo conectar a ningún servidor. Intentando de nuevo en 5s: %v", err))
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// Esperar una solicitud del cliente
		msg, ok := <-requests
		if !ok {
			logEvent("NETWORK_HANDLER", "SHUTDOWN", "Canal de solicitudes cerrado. Terminando.")
			if conn != nil {
				conn.Close()
			}
			return
		}

		msgBytes, err := json.Marshal(msg)
		if err != nil {
			logEvent("NETWORK_HANDLER", "ERROR", fmt.Sprintf("Falla al serializar mensaje: %v", err))
			continue
		}

		// Lógica de reintento para la escritura y lectura
		for i := 0; i < 3; i++ {
			_, err = conn.Write(msgBytes)
			if err != nil {
				logEvent("NETWORK_HANDLER", "WRITE_ERROR", fmt.Sprintf("Falla al enviar mensaje: %v. Reintentando...", err))
				conn.Close()
				conn = nil
				break // Salir del bucle interno y volver a intentar con una nueva conexión
			}

			buffer := make([]byte, 4096)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				logEvent("NETWORK_HANDLER", "TIMEOUT", "Error: El servidor no respondió a tiempo. Reintentando...")
				conn.Close()
				conn = nil
				break // Salir y volver a intentar con una nueva conexión
			}

			var responseMsg NetworkMessage
			if err := json.Unmarshal(buffer[:n], &responseMsg); err != nil {
				logEvent("NETWORK_HANDLER", "ERROR", fmt.Sprintf("Falla al deserializar la respuesta: %v", err))
				responses <- NetworkMessage{Type: "NACK", Payload: []byte("Error al procesar la respuesta.")}
				break
			}

			responses <- responseMsg
			break // Éxito, salir del bucle de reintento
		}
	}
}

func main() {
	requests := make(chan NetworkMessage)
	responses := make(chan NetworkMessage, 100)

	go networkHandler(requests, responses)

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido - Modo CLI")
	fmt.Println("Comandos: list, get <nombre>, add <nombre>, edit <nombre>, exit")

	go func() {
		for {
			response, ok := <-responses
			if !ok {
				return
			}
			processResponse(response)
		}
	}()

	for {
		fmt.Print("-> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		var msg NetworkMessage

		switch command {
		case "list":
			msg = NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
			requests <- msg
		case "get":
			if len(parts) < 2 {
				fmt.Println("Uso: get <nombre_archivo>")
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
			requests <- msg
		case "add":
			if len(parts) < 2 {
				fmt.Println("Uso: add <nombre_archivo>")
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "ADD_FILE", Payload: fileNameBytes}
			requests <- msg
		case "edit":
			if len(parts) < 2 {
				fmt.Println("Uso: edit <nombre_archivo>")
				continue
			}
			fileName := parts[1]
			go handleEditCommand(fileName, requests, responses)
		case "exit":
			close(requests)
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			return
		default:
			fmt.Println("Comando no reconocido.")
			continue
		}
	}
}

func processResponse(responseMsg NetworkMessage) {
	// ... (La misma lógica de processResponse que ya tenías)
	switch responseMsg.Type {
	case "RESPONSE_LIST":
		mu.Lock()
		var directory map[string]DirectoryEntry
		json.Unmarshal(responseMsg.Payload, &directory)
		localDirectory = directory
		mu.Unlock()
		logEvent("CLIENT", "RESPONSE_LIST_RECEIVED", fmt.Sprintf("Lista de %d archivos recibida y guardada localmente.", len(localDirectory)))
		fmt.Println("\n--- Archivos Compartidos (Actualizados) ---")
		for name, entry := range localDirectory {
			fmt.Printf("- Nombre: %s, Tamaño: %d bytes, Dueño: %s, Versión: %d\n", name, entry.Size, entry.OwnerIP, entry.Version)
		}
		fmt.Println("----------------------------------------\n")
	case "RESPONSE":
		mu.Lock()
		var entry DirectoryEntry
		json.Unmarshal(responseMsg.Payload, &entry)
		localDirectory[entry.FileName] = entry
		mu.Unlock()
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

func handleEditCommand(fileName string, requests chan<- NetworkMessage, responses <-chan NetworkMessage) {
	// 1. Obtener el archivo
	fileNameBytes, _ := json.Marshal(fileName)
	requestMsg := NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
	requests <- requestMsg

	responseMsg := <-responses

	if responseMsg.Type == "FILE_RESPONSE" {
		tempFile := "edit_" + fileName
		if err := os.WriteFile(tempFile, responseMsg.Payload, 0644); err != nil {
			logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al guardar el archivo temporal: %v", err))
			return
		}

		fmt.Printf("Archivo descargado y guardado como '%s'.\n", tempFile)
		fmt.Printf("Por favor, edita el archivo y presiona Enter para subir los cambios.\n")
		bufio.NewReader(os.Stdin).ReadString('\n')

		modifiedContent, err := os.ReadFile(tempFile)
		if err != nil {
			logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al leer el archivo modificado: %v", err))
			os.Remove(tempFile)
			return
		}

		mu.Lock()
		entry, found := localDirectory[fileName]
		mu.Unlock()
		if !found {
			logEvent("CLIENT", "ERROR", "No se encontró la entrada del archivo en la caché local para la versión.")
			os.Remove(tempFile)
			return
		}

		fileUpdate := FileUpdate{
			FileName:         fileName,
			Content:          modifiedContent,
			ModificationDate: time.Now(),
			Version:          entry.Version,
		}
		payloadBytes, _ := json.Marshal(fileUpdate)
		msg := NetworkMessage{Type: "FILE_WRITE_UPDATE", Payload: payloadBytes}
		requests <- msg

		os.Remove(tempFile)
	} else {
		fmt.Println("Error del servidor:", string(responseMsg.Payload))
	}
}
