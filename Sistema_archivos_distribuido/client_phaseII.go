package main

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/pion/dtls/v2"
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
	file, err := os.OpenFile("client.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error al abrir el archivo de log: %v", err)
		return
	}
	defer file.Close()
	file.Write(logBytes)
	file.WriteString("\n")
}

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
		InsecureSkipVerify: false,
		RootCAs:            roots,
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

	// Ejemplo de petición: Obtener la lista completa
	requestList := NetworkMessage{
		Type:    "GET_FULL_LIST",
		Payload: []byte{},
	}
	requestListBytes, _ := json.Marshal(requestList)
	conn.Write(requestListBytes)
	logEvent("CLIENT", "REQUEST_SENT", "Solicitud de lista completa enviada.")
	time.Sleep(1 * time.Second)

	// Ejemplo de petición: Obtener información de un archivo específico
	requestFile := "test_file.txt"
	requestFileBytes, _ := json.Marshal(requestFile)
	requestMsg := NetworkMessage{
		Type: "GET_FILE_INFO",
		Payload: requestFileBytes,
	}
	requestMsgBytes, _ := json.Marshal(requestMsg)
	conn.Write(requestMsgBytes)
	logEvent("CLIENT", "REQUEST_SENT", fmt.Sprintf("Solicitud de información para '%s' enviada.", requestFile))
	
	// Bucle para recibir respuestas
	buffer := make([]byte, 2048)
	for i := 0; i < 2; i++ {
		n, err := conn.Read(buffer)
		if err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al recibir respuesta: %v", err))
			return
		}

		var responseMsg NetworkMessage
		if err := json.Unmarshal(buffer[:n], &responseMsg); err != nil {
			logEvent("CLIENT", "ERROR", fmt.Sprintf("Respuesta malformada: %v", err))
			continue
		}

		switch responseMsg.Type {
		case "RESPONSE":
			var entry DirectoryEntry
			if err := json.Unmarshal(responseMsg.Payload, &entry); err != nil {
				logEvent("CLIENT", "ERROR", "Falla al decodificar la entrada del directorio.")
				continue
			}
			logEvent("CLIENT", "RESPONSE_RECEIVED", fmt.Sprintf("Información recibida para '%s' (Autoritativa: %t)", entry.FileName, responseMsg.Authoritative))
		case "RESPONSE_LIST":
			var list map[string]DirectoryEntry
			if err := json.Unmarshal(responseMsg.Payload, &list); err != nil {
				logEvent("CLIENT", "ERROR", "Falla al decodificar la lista de directorios.")
				continue
			}
			logEvent("CLIENT", "RESPONSE_LIST_RECEIVED", fmt.Sprintf("Lista de %d archivos recibida.", len(list)))
			for _, entry := range list {
				logEvent("CLIENT", "LIST_ENTRY", fmt.Sprintf("Archivo: %s, Tamaño: %d bytes, TTL: %d", entry.FileName, entry.Size, entry.TTL))
			}
		case "NACK":
			logEvent("CLIENT", "NACK_RECEIVED", fmt.Sprintf("Petición rechazada: %s", string(responseMsg.Payload)))
		}
	}
}
