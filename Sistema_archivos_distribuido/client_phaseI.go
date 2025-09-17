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

// logEntry define la estructura para nuestros logs.
type logEntry struct {
	Timestamp time.Time
	Module    string // e.g., "CLIENT"
	Action    string // e.g., "HANDSHAKE_STARTED"
	Details   string
}

// logToConsole y al archivo de log
func logEvent(module, action, details string) {
	entry := logEntry{
		Timestamp: time.Now(),
		Module:    module,
		Action:    action,
		Details:   details,
	}

	// Serializar a JSON para un formato consistente
	logBytes, err := json.Marshal(entry)
	if err != nil {
		log.Printf("Error al serializar log: %v", err)
		return
	}

	// Imprimir en consola y escribir en archivo
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
	// 1. Cargar el certificado del servidor para la validación de seguridad
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

	// 2. Configuración DTLS con validación de certificado
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

	// 3. Conectarse al servidor DTLS
	logEvent("CLIENT", "HANDSHAKE_STARTED", "Iniciando handshake DTLS...")
	conn, err := dtls.Dial("udp", addr, dtlsConfig)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla en el handshake DTLS: %v", err))
		panic(err)
	}
	defer conn.Close()
	logEvent("CLIENT", "HANDSHAKE_COMPLETED", "Handshake DTLS completado y certificado validado.")

	// 4. Enviar un mensaje de prueba
	msg := NetworkMessage{
		Type:    "TEST",
		Payload: []byte("¡Hola servidor!"),
		Authoritative: false,
		SenderIP: conn.LocalAddr().String(),
	}
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al serializar mensaje: %v", err))
		panic(err)
	}
	if _, err := conn.Write(msgBytes); err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al enviar mensaje: %v", err))
		panic(err)
	}
	logEvent("CLIENT", "MESSAGE_SENT", fmt.Sprintf("Mensaje enviado: %s", msg.Type))

	// 5. Esperar una respuesta
	buffer := make([]byte, 2048)
	n, err := conn.Read(buffer)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al recibir respuesta: %v", err))
		return
	}
	var responseMsg NetworkMessage
	if err := json.Unmarshal(buffer[:n], &responseMsg); err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Respuesta malformada: %v", err))
		return
	}
	logEvent("CLIENT", "MESSAGE_RECEIVED", fmt.Sprintf("De servidor, tipo: %s, payload: %s", responseMsg.Type, string(responseMsg.Payload)))
}
