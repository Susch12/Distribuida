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

	// Escenario de prueba para Fase 3
	// Simulamos un escenario donde el servidor no tiene el archivo y responde de forma no autoritativa.
	
	// Paso 1: El cliente solicita un archivo que el servidor no tiene (ej. "new_file.txt").
	// En la vida real, el servidor tendría un registro del dueño.
	requestFile := "new_file.txt"
	requestFileBytes, _ := json.Marshal(requestFile)
	requestMsg := NetworkMessage{
		Type: "GET_FILE_INFO",
		Payload: requestFileBytes,
		Authoritative: false,
		SenderIP: conn.LocalAddr().String(),
	}
	requestMsgBytes, _ := json.Marshal(requestMsg)
	conn.Write(requestMsgBytes)
	logEvent("CLIENT", "REQUEST_SENT", fmt.Sprintf("Solicitud de info para '%s' enviada.", requestFile))

	// Paso 2: El cliente recibe una respuesta del servidor.
	buffer := make([]byte, 2048)
	n, err := conn.Read(buffer)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al recibir respuesta del servidor: %v", err))
		return
	}
	var responseMsg NetworkMessage
	if err := json.Unmarshal(buffer[:n], &responseMsg); err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Respuesta malformada: %v", err))
		return
	}

	// Paso 3: Simular una respuesta no autoritativa.
	// Nota: El servidor de la Fase 2 solo responde NACK si no tiene el archivo.
	// Para simular la fase 3, asumimos que el cliente recibe una respuesta "no autoritativa"
	// que le indica la IP del dueño.
	
	ownerIP := "localhost:8081" // Simula que el dueño está en otro puerto/servidor
	logEvent("CLIENT", "RESPONSE_RECEIVED_NON_AUTH", fmt.Sprintf("Respuesta no autoritativa. Pidiendo copia al dueño en %s", ownerIP))
	
	// Paso 4: El cliente ahora se conecta al dueño (simulamos otro servidor en otro puerto)
	connOwner, err := dtls.Dial("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081}, dtlsConfig)

	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al conectar con el dueño: %v", err))
		return
	}
	defer connOwner.Close()
	logEvent("CLIENT", "HANDSHAKE_COMPLETED", fmt.Sprintf("Conectado con el dueño en %s", ownerIP))
	
	// Paso 5: El cliente pide la copia al dueño
	requestCopyMsg := NetworkMessage{
		Type: "REQUEST_FILE_COPY",
		Payload: []byte(requestFile),
	}
	requestCopyBytes, _ := json.Marshal(requestCopyMsg)
	connOwner.Write(requestCopyBytes)
	logEvent("CLIENT", "REQUEST_SENT", "Solicitud de copia de archivo enviada al dueño.")
	
	// Paso 6: Recibir la copia del dueño
	n, err = connOwner.Read(buffer)
	if err != nil {
		logEvent("CLIENT", "ERROR", fmt.Sprintf("Falla al recibir copia del dueño: %v", err))
		return
	}
	var fileCopyResponse NetworkMessage
	json.Unmarshal(buffer[:n], &fileCopyResponse)
	logEvent("CLIENT", "FILE_RECEIVED", fmt.Sprintf("Copia de archivo recibida: %s", string(fileCopyResponse.Payload)))
	
	// Paso 7: Simular edición local
	time.Sleep(1 * time.Second) // Simular trabajo
	modifiedContent := string(fileCopyResponse.Payload) + " - editado por el cliente."
	
	// Paso 8: El cliente envía la actualización de vuelta al dueño
	updatedEntry := DirectoryEntry{
		FileName: requestFile,
		Size: int64(len(modifiedContent)),
		ModificationDate: time.Now(),
		OwnerIP: connOwner.LocalAddr().String(),
	}
	updatedEntryBytes, _ := json.Marshal(updatedEntry)
	updateMsg := NetworkMessage{
		Type: "FILE_COPY_UPDATE",
		Payload: updatedEntryBytes,
		Authoritative: true, // La actualización es autoritativa
	}
	updateBytes, _ := json.Marshal(updateMsg)
	connOwner.Write(updateBytes)
	logEvent("CLIENT", "UPDATE_SENT", "Actualización enviada al dueño.")
	
	// Paso 9: Esperar ACK de la actualización
	connOwner.Read(buffer)
	logEvent("CLIENT", "UPDATE_ACK_RECEIVED", "ACK de actualización recibido del dueño.")
}
