package main

import (
	"crypto/tls"
	"encoding/json"
	"encoding/pem" // Add this line
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pion/dtls/v2"
	"github.com/pion/dtls/v2/pkg/crypto/selfsign"
)

// logEntry define la estructura para nuestros logs.
type logEntry struct {
	Timestamp time.Time
	Module    string // e.g., "SERVER"
	Action    string // e.g., "HANDSHAKE_COMPLETED"
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
	
	file, err := os.OpenFile("server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error al abrir el archivo de log: %v", err)
		return
	}
	defer file.Close()
	file.Write(logBytes)
	file.WriteString("\n")
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

	// 2. Configurar el listener DTLS
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

	// 3. Aceptar conexiones de clientes en goroutines
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

	// Bucle para recibir mensajes del cliente
	buffer := make([]byte, 2048)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			logEvent("SERVER", "CONNECTION_CLOSED", fmt.Sprintf("Conexión con %s cerrada: %v", conn.RemoteAddr(), err))
			return
		}
		
		var msg NetworkMessage
		if err := json.Unmarshal(buffer[:n], &msg); err != nil {
			logEvent("SERVER", "ERROR", fmt.Sprintf("Mensaje malformado de %s: %v", conn.RemoteAddr(), err))
			continue
		}
		
		// Lógica base para manejar mensajes
		logEvent("SERVER", "MESSAGE_RECEIVED", fmt.Sprintf("De %s, tipo: %s", conn.RemoteAddr(), msg.Type))

		// Enviar una respuesta de prueba
		responseMsg := NetworkMessage{
			Type:    "ACK",
			Payload: []byte("Mensaje recibido, handshake exitoso."),
			Authoritative: true,
			SenderIP: conn.LocalAddr().String(),
		}
		responseBytes, _ := json.Marshal(responseMsg)
		conn.Write(responseBytes)
	}
}
