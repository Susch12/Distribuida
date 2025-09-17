package main

import (
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pion/dtls/v2"
	"github.com/pion/dtls/v2/pkg/crypto/selfsign"
)

// logEntry y logEvent se mantienen igual
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
	file, err := os.OpenFile("server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error al abrir el archivo de log: %v", err)
		return
	}
	defer file.Close()
	file.Write(logBytes)
	file.WriteString("\n")
}

// Global state
var (
	sharedFilesMutex sync.RWMutex
	sharedFiles      = make(map[string]DirectoryEntry)
)

// go cleaner() se encarga de verificar la disponibilidad de los archivos.
func cleaner() {
	ticker := time.NewTicker(30 * time.Second) // Verificar cada 30 segundos
	defer ticker.Stop()

	for range ticker.C {
		logEvent("SERVER_CLEANER", "SCAN_START", "Iniciando escaneo de archivos compartidos.")
		sharedFilesMutex.Lock()
		for key, entry := range sharedFiles {
			// Simulación de verificación de TTL
			// En la fase 3, esta lógica se expandirá para preguntar a otros servidores.
			if entry.TTL > 0 {
				entry.TTL -= 30
				if entry.TTL <= 0 {
					logEvent("SERVER_CLEANER", "RECORD_EXPIRED", fmt.Sprintf("Eliminado registro para '%s' por TTL expirado.", key))
					delete(sharedFiles, key)
				} else {
					sharedFiles[key] = entry
					logEvent("SERVER_CLEANER", "TTL_UPDATE", fmt.Sprintf("Actualizado TTL para '%s', nuevo TTL: %d", key, entry.TTL))
				}
			}
		}
		sharedFilesMutex.Unlock()
	}
}

func main() {
	// 1. Generar y guardar el certificado DTLS
	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al generar certificado: %v", err))
		panic(err)
	}
	certOut, err := os.Create("cert.pem")
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al crear cert.pem: %v", err))
		panic(err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}); err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al codificar certificado: %v", err))
		panic(err)
	}
	logEvent("SERVER", "CERT_CREATED", "Certificado DTLS generado y guardado en cert.pem")

	// 2. Llenar el directorio con datos de prueba
	sharedFiles["test_file.txt"] = DirectoryEntry{
		FileName:        "test_file.txt",
		Size:            123,
		ModificationDate: time.Now(),
		TTL:             60, // 60 segundos
		OwnerIP:         "127.0.0.1",
	}
	sharedFiles["perpetual_file.doc"] = DirectoryEntry{
		FileName:        "perpetual_file.doc",
		Size:            456,
		ModificationDate: time.Now(),
		TTL:             0, // TTL=0, no se actualiza
		OwnerIP:         "127.0.0.1",
	}
	logEvent("SERVER", "DIRECTORY_INIT", "Directorio inicializado con archivos de prueba.")

	// Iniciar el limpiador de TTL en una goroutine
	go cleaner()

	// 3. Configurar el listener DTLS
	dtlsConfig := &dtls.Config{
		Certificates:         []tls.Certificate{certificate},
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}
	addr, err := net.ResolveUDPAddr("udp", ":8080")
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al resolver dirección: %v", err))
		panic(err)
	}
	listener, err := dtls.Listen("udp", addr, dtlsConfig)
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al escuchar DTLS: %v", err))
		panic(err)
	}
	defer listener.Close()
	logEvent("SERVER", "LISTENING", "Servidor DTLS escuchando en el puerto 8080...")

	// 4. Aceptar conexiones de clientes en goroutines
	var wg sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al aceptar conexión: %v", err))
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleClient(conn)
		}()
	}
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	logEvent("SERVER", "HANDSHAKE_COMPLETED", fmt.Sprintf("Nuevo cliente conectado desde %s", conn.RemoteAddr()))

	buffer := make([]byte, 2048)
	n, err := conn.Read(buffer)
	if err != nil {
		logEvent("SERVER", "CONNECTION_CLOSED", fmt.Sprintf("Conexión con %s cerrada: %v", conn.RemoteAddr(), err))
		return
	}
	
	var msg NetworkMessage
	if err := json.Unmarshal(buffer[:n], &msg); err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Mensaje malformado de %s: %v", conn.RemoteAddr(), err))
		return
	}
	
	logEvent("SERVER", "MESSAGE_RECEIVED", fmt.Sprintf("De %s, tipo: %s", conn.RemoteAddr(), msg.Type))

	var responseMsg NetworkMessage
	switch msg.Type {
	case "GET_FILE_INFO":
		var fileName string
		json.Unmarshal(msg.Payload, &fileName)
		logEvent("SERVER", "QUERY", fmt.Sprintf("Consulta de información para '%s'", fileName))
		
		sharedFilesMutex.RLock()
		entry, found := sharedFiles[fileName]
		sharedFilesMutex.RUnlock()
		
		if found {
			// El servidor es el dueño del archivo (para la prueba, asumimos que sí)
			payloadBytes, _ := json.Marshal(entry)
			responseMsg = NetworkMessage{
				Type:    "RESPONSE",
				Payload: payloadBytes,
				Authoritative: true,
				SenderIP: conn.LocalAddr().String(),
			}
		} else {
			responseMsg = NetworkMessage{
				Type:    "NACK",
				Payload: []byte("Archivo no encontrado en el directorio."),
				Authoritative: false,
				SenderIP: conn.LocalAddr().String(),
			}
		}
	case "GET_FULL_LIST":
		logEvent("SERVER", "QUERY_LIST", "Solicitud de lista completa")
		
		sharedFilesMutex.RLock()
		payloadBytes, _ := json.Marshal(sharedFiles)
		sharedFilesMutex.RUnlock()
		
		responseMsg = NetworkMessage{
			Type:    "RESPONSE_LIST",
			Payload: payloadBytes,
			Authoritative: true,
			SenderIP: conn.LocalAddr().String(),
		}
	default:
		responseMsg = NetworkMessage{
			Type: "NACK",
			Payload: []byte("Tipo de petición no reconocido."),
			Authoritative: false,
			SenderIP: conn.LocalAddr().String(),
		}
	}

	responseBytes, _ := json.Marshal(responseMsg)
	conn.Write(responseBytes)
	logEvent("SERVER", "MESSAGE_SENT", fmt.Sprintf("Respuesta enviada de tipo: %s", responseMsg.Type))
}
