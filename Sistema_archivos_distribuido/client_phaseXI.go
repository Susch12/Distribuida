package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
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

type DirectoryEntry struct {
	FileName         string
	Size             int64
	ModificationDate time.Time
	Version          int
	TTL              int
	OwnerIP          string
}

type NetworkMessage struct {
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
	Authoritative bool            `json:"authoritative"`
	SenderIP      string          `json:"senderIp"`
}

type FileUpdate struct {
	FileName         string
	Content          []byte
	Version          int
	ModificationDate time.Time
}

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

func connectToServer(serverAddr string) (*dtls.Conn, error) {
	dtlsConfig, err := getDTLSConfig()
	if err != nil {
		return nil, err
	}
	addr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("falla al resolver dirección: %v", err)
	}
	conn, err := dtls.Dial("udp", addr, dtlsConfig)
	if err != nil {
		return nil, fmt.Errorf("falla al conectar: %v", err)
	}
	return conn, nil
}

func getFile(fileName, serverAddr string) (*DirectoryEntry, error) {
	conn, err := connectToServer(serverAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	payloadBytes, _ := json.Marshal(fileName)
	msg := NetworkMessage{
		Type:    "GET_FILE_INFO",
		Payload: payloadBytes,
	}
	msgBytes, _ := json.Marshal(msg)
	
	_, err = conn.Write(msgBytes)
	if err != nil {
		return nil, fmt.Errorf("falla al enviar mensaje: %v", err)
	}

	buffer := make([]byte, 2048)
	n, _, err := conn.ReadFrom(buffer)
	if err != nil {
		return nil, fmt.Errorf("falla al leer respuesta: %v", err)
	}

	var response NetworkMessage
	json.Unmarshal(buffer[:n], &response)

	if response.Type == "RESPONSE" && response.Authoritative {
		var entry DirectoryEntry
		json.Unmarshal(response.Payload, &entry)
		logEvent("CLIENT", "INFO_RECEIVED", fmt.Sprintf("Información autoritativa para '%s' recibida.", fileName))
		return &entry, nil
	} else if response.Type == "NACK" {
		logEvent("CLIENT", "NACK", fmt.Sprintf("Servidor %s respondió NACK para '%s'.", serverAddr, fileName))
		return nil, fmt.Errorf("archivo no encontrado en el servidor")
	} else {
		return nil, fmt.Errorf("respuesta inesperada del servidor")
	}
}

func requestFileContent(fileName, ownerAddr string) ([]byte, error) {
	conn, err := connectToServer(ownerAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	
	payloadBytes, _ := json.Marshal(fileName)
	msg := NetworkMessage{
		Type:    "REQUEST_FILE",
		Payload: payloadBytes,
	}
	msgBytes, _ := json.Marshal(msg)
	
	_, err = conn.Write(msgBytes)
	if err != nil {
		return nil, fmt.Errorf("falla al solicitar archivo: %v", err)
	}
	
	buffer := make([]byte, 4096)
	n, _, err := conn.ReadFrom(buffer)
	if err != nil {
		return nil, fmt.Errorf("falla al leer contenido del archivo: %v", err)
	}

	var response NetworkMessage
	json.Unmarshal(buffer[:n], &response)

	if response.Type == "FILE_RESPONSE" {
		logEvent("CLIENT", "FILE_RECEIVED", fmt.Sprintf("Contenido del archivo '%s' recibido del dueño.", fileName))
		return response.Payload, nil
	} else if response.Type == "REDIRECT_OWNER" {
		var newOwnerIP string
		json.Unmarshal(response.Payload, &newOwnerIP)
		logEvent("CLIENT", "REDIRECT", fmt.Sprintf("Redireccionado a nuevo dueño: %s. Reintentando la petición.", newOwnerIP))
		return requestFileContent(fileName, newOwnerIP) // Recursivamente llama a la función con la nueva IP
	} else {
		return nil, fmt.Errorf("respuesta inesperada del servidor")
	}
}

func editAndSyncFile(fileName string, entry DirectoryEntry) {
	// Fase de "Unit of Work": Leer, editar y guardar en copia local
	tempFileName := "temp_" + fileName
	err := os.WriteFile(tempFileName, []byte(""), 0644) // Crear archivo temporal
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("No se pudo crear el archivo temporal: %v", err))
		return
	}
	
	fmt.Printf("Contenido actual de '%s':\n", tempFileName)
	fmt.Println(string(entry.Size)) // Contenido de ejemplo
	fmt.Println("Puedes editar el archivo temporal. Presiona Enter cuando termines:")
	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n') // Pausa para permitir edición
	
	updatedContent, err := os.ReadFile(tempFileName)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("No se pudo leer el archivo temporal: %v", err))
		return
	}
	
	// Fase de Sincronización
	conn, err := connectToServer(entry.OwnerIP)
	if err != nil {
		logEvent("CLIENT", "SYNC_ERROR", fmt.Sprintf("No se pudo conectar para sincronizar: %v", err))
		os.Remove(tempFileName)
		return
	}
	defer conn.Close()

	fileUpdate := FileUpdate{
		FileName:         fileName,
		Content:          updatedContent,
		Version:          entry.Version,
		ModificationDate: time.Now(),
	}
	payloadBytes, _ := json.Marshal(fileUpdate)
	msg := NetworkMessage{
		Type:    "FILE_WRITE_UPDATE",
		Payload: payloadBytes,
	}
	msgBytes, _ := json.Marshal(msg)
	
	_, err = conn.Write(msgBytes)
	if err != nil {
		logEvent("CLIENT", "SYNC_ERROR", fmt.Sprintf("Falla al enviar actualización: %v", err))
		return
	}

	buffer := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buffer)
	if err != nil {
		logEvent("CLIENT", "SYNC_ERROR", fmt.Sprintf("Falla al leer respuesta de sincronización: %v", err))
		return
	}
	
	var response NetworkMessage
	json.Unmarshal(buffer[:n], &response)
	
	if response.Type == "UPDATE_ACK" {
		logEvent("CLIENT", "SYNC_SUCCESS", fmt.Sprintf("Sincronización de '%s' exitosa. Se eliminó la copia local.", fileName))
		os.Remove(tempFileName)
	} else {
		logEvent("CLIENT", "SYNC_FAILURE", fmt.Sprintf("Sincronización de '%s' fallida. Razón: %s", fileName, string(response.Payload)))
	}
}

func main() {
	serverAddr := flag.String("server", "localhost:8080", "Dirección del servidor al que conectarse (ej: localhost:8080)")
	command := flag.String("command", "list", "Comando a ejecutar: 'list', 'info <file>', o 'edit <file>'")
	flag.Parse()

	args := flag.Args()

	switch *command {
	case "list":
		conn, err := connectToServer(*serverAddr)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Error de conexión: %v", err))
			return
		}
		defer conn.Close()

		msg := NetworkMessage{Type: "GET_FULL_LIST"}
		msgBytes, _ := json.Marshal(msg)
		
		_, err = conn.Write(msgBytes)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al enviar mensaje de lista: %v", err))
			return
		}

		buffer := make([]byte, 4096)
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al leer respuesta: %v", err))
			return
		}

		var response NetworkMessage
		json.Unmarshal(buffer[:n], &response)
		
		if response.Type == "RESPONSE_LIST" {
			var directory map[string]DirectoryEntry
			json.Unmarshal(response.Payload, &directory)
			logEvent("CLIENT", "LIST_RECEIVED", "Lista de archivos recibida:")
			for _, entry := range directory {
				fmt.Printf("  - Nombre: %s, Tamaño: %d B, Dueño: %s, Versión: %d\n", entry.FileName, entry.Size, entry.OwnerIP, entry.Version)
			}
		} else {
			logEvent("CLIENT", "ERROR", "Respuesta de lista inesperada.")
		}

	case "info":
		if len(args) < 1 {
			log.Fatal("Se requiere el nombre del archivo. Uso: go run client.go --command=info <nombre_archivo>")
		}
		fileName := args[0]
		entry, err := getFile(fileName, *serverAddr)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("No se pudo obtener la información de '%s': %v", fileName, err))
		} else {
			fmt.Printf("Información de '%s':\n", fileName)
			fmt.Printf("  - Tamaño: %d B\n", entry.Size)
			fmt.Printf("  - Modificación: %v\n", entry.ModificationDate)
			fmt.Printf("  - Versión: %d\n", entry.Version)
			fmt.Printf("  - TTL: %d s\n", entry.TTL)
			fmt.Printf("  - Dueño: %s\n", entry.OwnerIP)
		}

	case "edit":
		if len(args) < 1 {
			log.Fatal("Se requiere el nombre del archivo. Uso: go run client.go --command=edit <nombre_archivo>")
		}
		fileName := args[0]
		entry, err := getFile(fileName, *serverAddr)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("No se pudo obtener la información de '%s': %v", fileName, err))
			return
		}
		
		fileContent, err := requestFileContent(fileName, entry.OwnerIP)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("No se pudo obtener el contenido de '%s': %v", fileName, err))
			return
		}
		
		// Guardar contenido y preparar para la edición local
		tempFileName := "temp_" + fileName
		err = os.WriteFile(tempFileName, fileContent, 0644)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al guardar copia local: %v", err))
			return
		}
		
		fmt.Println("Archivo copiado localmente para edición. Por favor, edita '", tempFileName, "' y guarda los cambios.")
		fmt.Println("Presiona Enter cuando termines de editar...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')

		// Leer el archivo modificado y sincronizar
		modifiedContent, err := os.ReadFile(tempFileName)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al leer la copia modificada: %v", err))
			return
		}
		
		// Sincronizar con el servidor dueño
		conn, err := connectToServer(entry.OwnerIP)
		if err != nil {
			logEvent("CLIENT", "SYNC_ERROR", fmt.Sprintf("No se pudo conectar al dueño para sincronizar: %v", err))
			return
		}
		defer conn.Close()

		fileUpdate := FileUpdate{
			FileName:         fileName,
			Content:          modifiedContent,
			Version:          entry.Version,
			ModificationDate: entry.ModificationDate,
		}
		
		payloadBytes, _ := json.Marshal(fileUpdate)
		msg := NetworkMessage{
			Type:    "FILE_WRITE_UPDATE",
			Payload: payloadBytes,
		}
		msgBytes, _ := json.Marshal(msg)
		_, err = conn.Write(msgBytes)
		if err != nil {
			logEvent("CLIENT", "SYNC_ERROR", fmt.Sprintf("Falla al enviar actualización: %v", err))
			return
		}
		
		buffer := make([]byte, 1024)
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			logEvent("CLIENT", "SYNC_ERROR", fmt.Sprintf("Falla al leer respuesta de sincronización: %v", err))
			return
		}

		var response NetworkMessage
		json.Unmarshal(buffer[:n], &response)
		
		if response.Type == "UPDATE_ACK" {
			logEvent("CLIENT", "SYNC_SUCCESS", fmt.Sprintf("Sincronización de '%s' exitosa. Eliminando copia local.", fileName))
			os.Remove(tempFileName)
		} else {
			logEvent("CLIENT", "SYNC_FAILURE", fmt.Sprintf("Sincronización de '%s' fallida. Razón: %s", fileName, string(response.Payload)))
		}

	case "add":
		if len(args) < 1 {
			log.Fatal("Se requiere el nombre del archivo. Uso: go run client.go --command=add <nombre_archivo>")
		}
		fileName := args[0]
		conn, err := connectToServer(*serverAddr)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Error de conexión: %v", err))
			return
		}
		defer conn.Close()

		payloadBytes, _ := json.Marshal(fileName)
		msg := NetworkMessage{
			Type:    "ADD_FILE",
			Payload: payloadBytes,
		}
		msgBytes, _ := json.Marshal(msg)
		
		_, err = conn.Write(msgBytes)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al enviar mensaje de adición: %v", err))
			return
		}
		
		buffer := make([]byte, 1024)
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al leer respuesta: %v", err))
			return
		}
		
		var response NetworkMessage
		json.Unmarshal(buffer[:n], &response)
		
		if response.Type == "UPDATE_ACK" {
			logEvent("CLIENT", "ADD_SUCCESS", fmt.Sprintf("Archivo '%s' agregado con éxito al directorio.", fileName))
		} else {
			logEvent("CLIENT", "ADD_FAILURE", fmt.Sprintf("No se pudo agregar el archivo: %s", string(response.Payload)))
		}
	}
}
