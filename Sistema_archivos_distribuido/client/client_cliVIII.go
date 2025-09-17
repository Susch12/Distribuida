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
	"os/exec"
	"runtime"
	"strings"
	"sync"
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
	knownServers   = []string{"localhost:8080"}
	localDirectory = make(map[string]DirectoryEntry)
	mu             sync.Mutex
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
	
	// Establecemos la conexión al inicio y la pasamos al manejador
	conn, err := connectToPeer()
	if err != nil {
		logEvent("CLIENT", "CRITICAL_ERROR", fmt.Sprintf("No se pudo iniciar el cliente: %v", err))
		return
	}
	
	go networkHandler(requests, responses, conn)

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido - Modo CLI")
	printMenu()

	// Goroutine para procesar las respuestas del servidor
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
		
		switch command {
		case "list":
			msg := NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
			requests <- msg
		case "get":
			if len(parts) < 2 {
				fmt.Println("Uso: get <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg := NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
			requests <- msg
		case "add":
			if len(parts) < 2 {
				fmt.Println("Uso: add <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg := NetworkMessage{Type: "ADD_FILE", Payload: fileNameBytes}
			requests <- msg
		case "edit":
			if len(parts) < 2 {
				fmt.Println("Uso: edit <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			// La lógica de 'edit' es secuencial, por lo que la manejamos aquí mismo
			editFile(fileName, requests, responses)

		case "view":
			if len(parts) < 2 {
				fmt.Println("Uso: view <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			fileNameBytes, _ := json.Marshal(fileName)
			msg := NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
			requests <- msg
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

func networkHandler(requests <-chan NetworkMessage, responses chan<- NetworkMessage, conn net.Conn) {
	// ... (la lógica de este handler es la misma que la del código anterior) ...
	defer close(responses)
	defer conn.Close()

	for msg := range requests {
		logEvent("NETWORK_HANDLER", "REQUEST_SENT", fmt.Sprintf("Enviando mensaje de tipo: %s", msg.Type))
		
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			logEvent("NETWORK_HANDLER", "ERROR", fmt.Sprintf("Falla al serializar mensaje: %v", err))
			continue
		}

		for i := 0; i < 3; i++ {
			_, err = conn.Write(msgBytes)
			if err != nil {
				logEvent("NETWORK_HANDLER", "WRITE_ERROR", fmt.Sprintf("Falla al enviar mensaje: %v. Reintentando...", err))
				time.Sleep(1 * time.Second) // Espera antes de reintentar
				continue
			}
			
			buffer := make([]byte, 4096)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				logEvent("NETWORK_HANDLER", "TIMEOUT", "Error: El servidor no respondió a tiempo. Reintentando...")
				time.Sleep(1 * time.Second) // Espera antes de reintentar
				continue
			}
			
			var responseMsg NetworkMessage
			if err := json.Unmarshal(buffer[:n], &responseMsg); err != nil {
				logEvent("NETWORK_HANDLER", "ERROR", fmt.Sprintf("Falla al deserializar la respuesta: %v", err))
				responses <- NetworkMessage{Type: "NACK", Payload: []byte("Error al procesar la respuesta.")}
				break
			}
			
			responses <- responseMsg
			break
		}
	}
}

func editFile(fileName string, requests chan NetworkMessage, responses chan NetworkMessage, conn net.Conn) {
	// Esta función ahora será la encargada de manejar el flujo de 'edit'
	// y se mantendrá en un bucle esperando los comandos del usuario
	// en lugar de usar goroutines.
	// ... (La lógica de edición completa va aquí) ...
	logEvent("CLIENT", "EDIT_FLOW", fmt.Sprintf("Iniciando flujo de edición para '%s'", fileName))
	
	// Primero, solicita la información del archivo
	msg := NetworkMessage{Type: "GET_FILE_INFO", Payload: []byte(fileName)}
	requests <- msg

	response := <-responses
	if response.Type != "RESPONSE" {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("No se pudo obtener la información de '%s'", fileName))
		return
	}
	var entry DirectoryEntry
	json.Unmarshal(response.Payload, &entry)

	// Luego, solicita el contenido del archivo
	msg = NetworkMessage{Type: "REQUEST_FILE", Payload: []byte(fileName)}
	requests <- msg
	response = <-responses
	
	if response.Type != "FILE_RESPONSE" {
		logEvent("CLIENT", "ERROR", "No se pudo obtener el contenido del archivo.")
		return
	}
	
	// Y así sucesivamente con el resto de la lógica de edición.
	// ... (Resto de la lógica de edición) ...
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
