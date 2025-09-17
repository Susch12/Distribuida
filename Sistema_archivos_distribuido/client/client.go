package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pion/dtls/v2"
)

type DirectoryEntry struct {
	FileName         string    `json:"file_name"`
	Size             int64     `json:"size"`
	ModificationDate time.Time `json:"modification_date"`
	Version          int       `json:"version"`
	TTL              int       `json:"ttl"`
	OwnerIP          string    `json:"owner_ip"`
}

type FileUpdate struct {
	FileName         string    `json:"file_name"`
	Content          []byte    `json:"content"`
	ModificationDate time.Time `json:"modification_date"`
	Version          int       `json:"version"`
}

type NetworkMessage struct {
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	Authoritative bool            `json:"authoritative"`
	SenderIP      string          `json:"sender_ip"`
}

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
	mu             sync.Mutex
	currentConn    *dtls.Conn
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
	
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(knownServers), func(i, j int) { knownServers[i], knownServers[j] = knownServers[j], knownServers[i] })
	
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

func sendMessage(conn *dtls.Conn, msg NetworkMessage) (NetworkMessage, error) {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return NetworkMessage{}, fmt.Errorf("fallo al serializar el mensaje: %v", err)
	}
	_, err = conn.Write(msgBytes)
	if err != nil {
		return NetworkMessage{}, fmt.Errorf("fallo al escribir en la conexión: %v", err)
	}
	buffer := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		return NetworkMessage{}, fmt.Errorf("fallo al leer de la conexión: %v", err)
	}
	var responseMsg NetworkMessage
	if err := json.Unmarshal(buffer[:n], &responseMsg); err != nil {
		return NetworkMessage{}, fmt.Errorf("fallo al deserializar la respuesta: %v", err)
	}
	return responseMsg, nil
}

func printMenu() {
	fmt.Println("Comandos: list, get <nombre>, add <nombre>, edit <nombre>, view <nombre>, exit")
	fmt.Print("-> ")
}

func processResponse(responseMsg NetworkMessage) {
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
		printMenu()

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
		printMenu()

	case "FILE_RESPONSE":
		fmt.Println("\n--- Contenido del Archivo ---")
		fmt.Println(string(responseMsg.Payload))
		fmt.Println("----------------------------\n")
		printMenu()

	case "UPDATE_ACK":
		fmt.Println("✅ Servidor:", string(responseMsg.Payload))
		printMenu()

	case "UPDATE_REJECTED":
		fmt.Println("❌ Servidor:", string(responseMsg.Payload))
		printMenu()

	case "NACK":
		fmt.Println("❌ Servidor:", string(responseMsg.Payload))
		printMenu()

	default:
		logEvent("CLIENT", "UNEXPECTED_RESPONSE", fmt.Sprintf("Respuesta inesperada del servidor: %s", responseMsg.Type))
		printMenu()
	}
}

func main() {
	requests := make(chan NetworkMessage)
	responses := make(chan NetworkMessage, 100)

	go networkHandler(requests, responses)

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Cliente de Directorio Distribuido - Modo CLI")
	printMenu()

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
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		var msg NetworkMessage
		var fileName string
		if len(parts) >= 2 {
			fileName = parts[1]
		}
		
		switch command {
		case "list":
			msg = NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
			requests <- msg
		case "get":
			if len(parts) < 2 {
				fmt.Println("Uso: get <nombre_archivo>")
				printMenu()
				continue
			}
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
			requests <- msg
		case "add":
			if len(parts) < 2 {
				fmt.Println("Uso: add <nombre_archivo>")
				printMenu()
				continue
			}
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "ADD_FILE", Payload: fileNameBytes}
			requests <- msg
		case "view":
			if len(parts) < 2 {
				fmt.Println("Uso: view <nombre_archivo>")
				printMenu()
				continue
			}
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
			requests <- msg
		case "edit":
			if len(parts) < 2 {
				fmt.Println("Uso: edit <nombre_archivo>")
				printMenu()
				continue
			}
			// La lógica de 'edit' es secuencial, por lo que la manejamos aquí mismo
			go handleEditCommand(fileName, requests, responses)

		case "exit":
			close(requests)
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			return
		default:
			fmt.Println("Comando no reconocido.")
			printMenu()
		}
	}
}

func handleEditCommand(fileName string, requests chan<- NetworkMessage, responses <-chan NetworkMessage) {
	// Primero, obtener la información del archivo para saber su versión
	fileNameBytes, _ := json.Marshal(fileName)
	requestInfoMsg := NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
	requests <- requestInfoMsg
	
	infoResponse := <-responses
	if infoResponse.Type != "RESPONSE" {
		fmt.Println("Error: No se pudo obtener la información del archivo para edición.")
		return
	}
	
	var entry DirectoryEntry
	json.Unmarshal(infoResponse.Payload, &entry)
	
	// Ahora, obtener el contenido del archivo
	requestFileMsg := NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
	requests <- requestFileMsg
	
	fileResponse := <-responses
	if fileResponse.Type != "FILE_RESPONSE" {
		fmt.Println("Error: No se pudo obtener el contenido del archivo para edición.")
		return
	}
	
	// Guardar el archivo temporalmente
	tempFile := "edit_" + fileName
	err := os.WriteFile(tempFile, fileResponse.Payload, 0644)
	if err != nil {
		logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al guardar archivo temporal: %v", err))
		return
	}
	defer os.Remove(tempFile)

	fmt.Printf("Archivo descargado y guardado como '%s'.\n", tempFile)
	fmt.Printf("Por favor, edita el archivo y presiona Enter para subir los cambios.\n")
	bufio.NewReader(os.Stdin).ReadString('\n')

	modifiedContent, err := os.ReadFile(tempFile)
	if err != nil {
		logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al leer el archivo modificado: %v", err))
		return
	}
	
	// Preparar el mensaje de actualización
	fileUpdate := FileUpdate{
		FileName:         fileName,
		Content:          modifiedContent,
		ModificationDate: time.Now(),
		Version:          entry.Version, // Usar la versión original para el control de concurrencia
	}
	payloadBytes, _ := json.Marshal(fileUpdate)
	msg := NetworkMessage{Type: "FILE_WRITE_UPDATE", Payload: payloadBytes}
	requests <- msg
	
	// Esperar la respuesta de la actualización
	updateResponse := <-responses
	
	if updateResponse.Type == "UPDATE_ACK" {
		fmt.Println("✅ Servidor:", string(updateResponse.Payload))
	} else {
		fmt.Println("❌ Servidor:", string(updateResponse.Payload))
	}
}

func getEditor() string {
	editor := os.Getenv("EDITOR")
	if editor != "" {
		return editor
	}

	if runtime.GOOS == "windows" {
		return "notepad.exe"
	}
	return "nano"
}
