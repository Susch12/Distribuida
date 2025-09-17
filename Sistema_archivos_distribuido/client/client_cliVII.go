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

// Nueva función que encapsula la lógica de reintento y envío
func executeCommand(msg NetworkMessage) (NetworkMessage, error) {
	if currentConn == nil {
		conn, err := connectToPeer()
		if err != nil {
			return NetworkMessage{}, fmt.Errorf("no se pudo conectar a ningún servidor: %v", err)
		}
		currentConn = conn
	}

	responseMsg, err := sendMessage(currentConn, msg)
	if err != nil {
		logEvent("CLIENT", "NETWORK_ERROR", fmt.Sprintf("Falla de red: %v. Intentando reconectar...", err))
		currentConn.Close()
		currentConn = nil
		newConn, reconnectErr := connectToPeer()
		if reconnectErr != nil {
			return NetworkMessage{}, fmt.Errorf("falla al reconectar: %v", reconnectErr)
		}
		currentConn = newConn
		// Reintentar con la nueva conexión
		responseMsg, err = sendMessage(currentConn, msg)
		if err != nil {
			return NetworkMessage{}, fmt.Errorf("el comando falló después de reconectar: %v", err)
		}
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
	var err error
	currentConn, err = connectToPeer()
	if err != nil {
		logEvent("CLIENT", "CRITICAL_ERROR", fmt.Sprintf("No se pudo iniciar el cliente: %v", err))
		return
	}
	defer currentConn.Close()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Cliente de Directorio Distribuido - Modo CLI")
	printMenu()

	for {
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		var msg NetworkMessage
		var responseMsg NetworkMessage

		switch command {
		case "list":
			msg = NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
			responseMsg, err = executeCommand(msg)
			if err != nil {
				logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al ejecutar comando: %v", err))
				continue
			}
			processResponse(responseMsg)
		case "add":
			if len(parts) < 2 {
				fmt.Println("Uso: add <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			payloadBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "ADD_FILE", Payload: payloadBytes}
			responseMsg, err = executeCommand(msg)
			if err != nil {
				logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al ejecutar comando: %v", err))
				continue
			}
			processResponse(responseMsg)
			if responseMsg.Type == "UPDATE_ACK" {
				listMsg := NetworkMessage{Type: "GET_FULL_LIST", Payload: []byte{}}
				listResponse, _ := executeCommand(listMsg)
				processResponse(listResponse)
			}
		case "view":
			if len(parts) < 2 {
				fmt.Println("Uso: view <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			payloadBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "REQUEST_FILE", Payload: payloadBytes}
			responseMsg, err = executeCommand(msg)
			if err != nil {
				logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al ejecutar comando: %v", err))
				continue
			}
			processResponse(responseMsg)
		case "edit":
			if len(parts) < 2 {
				fmt.Println("Uso: edit <nombre_archivo>")
				printMenu()
				continue
			}
			fileName := parts[1]
			mu.Lock()
			entry, found := localDirectory[fileName]
			mu.Unlock()
			if !found {
				logEvent("CLIENT", "ERROR", "Archivo no encontrado en la caché local. Intenta 'list' primero.")
				printMenu()
				continue
			}
			payloadBytes, _ := json.Marshal(fileName)
			msg = NetworkMessage{Type: "REQUEST_FILE", Payload: payloadBytes}
			responseMsg, err = executeCommand(msg)
			if err != nil {
				logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al ejecutar comando: %v", err))
				continue
			}
			if responseMsg.Type != "FILE_RESPONSE" {
				processResponse(responseMsg)
				continue
			}
			tempFile := "edit_" + fileName
			if err := os.WriteFile(tempFile, responseMsg.Payload, 0644); err != nil {
				logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al guardar archivo temporal: %v", err))
				os.Remove(tempFile)
				continue
			}
			editor := getEditor()
			cmd := exec.Command(editor, tempFile)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			fmt.Printf("Abriendo '%s' en el editor. Guarda y cierra para continuar.\n", tempFile)
			if err := cmd.Run(); err != nil {
				logEvent("CLIENT", "EDITOR_ERROR", fmt.Sprintf("Error al ejecutar el editor: %v", err))
				os.Remove(tempFile)
				continue
			}
			modifiedContent, err := os.ReadFile(tempFile)
			if err != nil {
				logEvent("CLIENT", "FILE_ERROR", fmt.Sprintf("Falla al leer el archivo modificado: %v", err))
				os.Remove(tempFile)
				continue
			}
			fileUpdate := FileUpdate{
				FileName:         fileName,
				Content:          modifiedContent,
				ModificationDate: time.Now(),
				Version:          entry.Version,
			}
			payloadBytes, _ = json.Marshal(fileUpdate)
			msg = NetworkMessage{Type: "FILE_WRITE_UPDATE", Payload: payloadBytes}
			updateResponse, err := executeCommand(msg)
			if err != nil {
				logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al ejecutar comando de actualización: %v", err))
				os.Remove(tempFile)
				continue
			}
			processResponse(updateResponse)
			os.Remove(tempFile)
			if updateResponse.Type == "UPDATE_ACK" {
				infoMsg := NetworkMessage{Type: "GET_FILE_INFO", Payload: payloadBytes}
				infoResponse, err := executeCommand(infoMsg)
				if err == nil {
					processResponse(infoResponse)
				}
			}
		case "exit":
			logEvent("CLIENT", "EXIT", "Cerrando cliente.")
			if currentConn != nil {
				currentConn.Close()
			}
			return
		default:
			fmt.Println("Comando no reconocido.")
			printMenu()
		}
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
