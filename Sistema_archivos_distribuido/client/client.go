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

// =============================================================================
// Funciones de Conexión y Red (Adaptadas del código unificado)
// =============================================================================

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
	knownServers   = []string{"192.168.100.136:8080"} // Lista de servidores a los que intentará conectarse
	localDirectory = make(map[string]DirectoryEntry)
	mu             sync.Mutex // Mutex para proteger localDirectory
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

// =============================================================================
// Lógica de Comandos y UI
// =============================================================================

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

// executeAndProcess se encarga de enviar el mensaje, manejar el reintento y procesar la respuesta.
func executeAndProcess(conn *dtls.Conn, msg NetworkMessage) {
	responseMsg, err := sendMessage(conn, msg)
	if err != nil {
		logEvent("CLIENT", "NETWORK_ERROR", fmt.Sprintf("Falla al ejecutar comando: %v", err))
		// Lógica de reconexión simplificada (el main se encarga de reconectar)
		return
	}
	processResponse(responseMsg)
}

func main() {
	var currentConn *dtls.Conn
	
	// Intenta la conexión inicial.
	conn, err := connectToPeer()
	if err != nil {
		logEvent("CLIENT", "CRITICAL_ERROR", fmt.Sprintf("No se pudo iniciar el cliente: %v", err))
		return
	}
	currentConn = conn
	defer currentConn.Close()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido - Modo CLI")
	printMenu()

	for {
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		var fileName string
		if len(parts) >= 2 {
			fileName = parts[1]
		}

		var msg NetworkMessage

		switch command {
		case "list":
			msg = NetworkMessage{Type: "GET_FULL_LIST"}
			executeAndProcess(currentConn, msg)
		
		case "get":
			if len(parts) < 2 {
				fmt.Println("Uso: get <nombre_archivo>")
				printMenu()
				continue
			}
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
			executeAndProcess(currentConn, msg)

		case "add":
			if len(parts) < 2 {
				fmt.Println("Uso: add <nombre_archivo>")
				printMenu()
				continue
			}
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "ADD_FILE", Payload: fileNameBytes}
			executeAndProcess(currentConn, msg)
			
		case "view":
			if len(parts) < 2 {
				fmt.Println("Uso: view <nombre_archivo>")
				printMenu()
				continue
			}
			fileNameBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
			executeAndProcess(currentConn, msg)

		case "edit":
			if len(parts) < 2 {
				fmt.Println("Uso: edit <nombre_archivo>")
				printMenu()
				continue
			}
			handleEditFlow(currentConn, fileName, reader)
			
		case "exit":
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			return

		default:
			fmt.Println("Comando no reconocido.")
			printMenu()
		}
	}
}

// handleEditFlow gestiona la secuencia de pasos para la edición de archivos.
func handleEditFlow(conn *dtls.Conn, fileName string, reader *bufio.Reader) {
	// 1. OBTENER INFORMACIÓN DE VERSIÓN (usando GET_FILE_INFO)
	fileNameBytes, _ := json.Marshal(fileName)
	infoMsg := NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
	infoResponse, err := sendMessage(conn, infoMsg)
	if err != nil || infoResponse.Type != "RESPONSE" {
		fmt.Println("❌ Error al obtener información del archivo para edición. Intente LIST primero.")
		return
	}
	var entry DirectoryEntry
	json.Unmarshal(infoResponse.Payload, &entry)

	// 2. OBTENER CONTENIDO DEL ARCHIVO (usando REQUEST_FILE)
	requestFileMsg := NetworkMessage{Type: "REQUEST_FILE", Payload: fileNameBytes}
	fileResponse, err := sendMessage(conn, requestFileMsg)
	if err != nil || fileResponse.Type != "FILE_RESPONSE" {
		fmt.Println("❌ Error al descargar el contenido del archivo.")
		return
	}
	
	// 3. UNIT OF WORK: Guardar, Editar y Leer.
	tempFile := "edit_" + fileName
	if err := os.WriteFile(tempFile, fileResponse.Payload, 0644); err != nil {
		logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al guardar archivo temporal: %v", err))
		return
	}
	
	fmt.Printf("Archivo descargado y guardado como '%s'.\n", tempFile)
	fmt.Printf(">>> Por favor, edita el archivo y presiona Enter para subir los cambios. <<<\n")

	// Ejecutar editor externo
	editor := getEditor()
	cmd := exec.Command(editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		logEvent("CLIENT", "EDITOR_ERROR", fmt.Sprintf("Error al ejecutar el editor: %v", err))
		os.Remove(tempFile)
		return
	}
	
	modifiedContent, err := os.ReadFile(tempFile)
	if err != nil {
		logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al leer el archivo modificado: %v", err))
		os.Remove(tempFile)
		return
	}
	
	// 4. SINCRONIZACIÓN
	fileUpdate := FileUpdate{
		FileName:         fileName,
		Content:          modifiedContent,
		ModificationDate: time.Now(),
		Version:          entry.Version, // Usar la versión original para la resolución de conflictos
	}
	payloadBytes, _ := json.Marshal(fileUpdate)
	updateMsg := NetworkMessage{Type: "FILE_WRITE_UPDATE", Payload: payloadBytes}
	updateResponse, err := sendMessage(conn, updateMsg)
	
	// 5. CLEANUP
	os.Remove(tempFile)
	
	if err != nil {
		logEvent("CLIENT", "NETWORK_ERROR", fmt.Sprintf("Falla al sincronizar: %v", err))
	} else if updateResponse.Type == "UPDATE_ACK" {
		fmt.Println("✅ Servidor:", string(updateResponse.Payload))
		// Opcional: Solicitar info actualizada para reflejar la Versión+1 en la caché
		infoUpdateMsg := NetworkMessage{Type: "GET_FILE_INFO", Payload: fileNameBytes}
		infoUpdateResponse, _ := sendMessage(conn, infoUpdateMsg)
		processResponse(infoUpdateResponse)
	} else {
		fmt.Println("❌ Servidor:", string(updateResponse.Payload))
	}
}
