package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag" // Importa la biblioteca 'flag'
	"fmt"
	"log"
	"net"
	"os"
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
	file, err := os.OpenFile("server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error al abrir el archivo de log: %v", err)
		return
	}
	defer file.Close()
	file.Write(logBytes)
	file.WriteString("\n")
}

var (
	sharedFilesMutex sync.RWMutex
	sharedFiles      = make(map[string]DirectoryEntry)
	knownPeers       = []string{"localhost:8081"}
	gossipProtocol   *GossipProtocol
)

func cleaner() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		logEvent("SERVER_CLEANER", "SCAN_START", "Iniciando escaneo de archivos compartidos.")
		keysToDelete := []string{}

		sharedFilesMutex.RLock()
		for key, entry := range sharedFiles {
			if entry.TTL > 0 {
				sharedFilesMutex.RUnlock()
				sharedFilesMutex.Lock()
				entry.TTL -= 30
				sharedFiles[key] = entry
				sharedFilesMutex.Unlock()
				sharedFilesMutex.RLock()

				logEvent("SERVER_CLEANER", "TTL_UPDATE", fmt.Sprintf("Actualizado TTL para '%s', nuevo TTL: %d", key, entry.TTL))

				if entry.TTL <= 0 {
					logEvent("SERVER_CLEANER", "TTL_EXPIRED", fmt.Sprintf("TTL expirado para '%s'. Verificando con otros peers...", key))

					foundNewOwner := false
					peersToCheck := gossipProtocol.GetRandomPeers(1)
					if len(peersToCheck) > 0 {
						newEntry, err := gossipProtocol.RequestStatus(key, peersToCheck[0])
						if err == nil {
							sharedFilesMutex.RUnlock()
							sharedFilesMutex.Lock()
							sharedFiles[key] = *newEntry
							sharedFilesMutex.Unlock()
							sharedFilesMutex.RLock()
							logEvent("SERVER_CLEANER", "OWNER_CHANGE", fmt.Sprintf("Se encontró un nuevo dueño para '%s': %s. Actualizando registro.", key, newEntry.OwnerIP))
							foundNewOwner = true
						}
					}
					if !foundNewOwner {
						keysToDelete = append(keysToDelete, key)
					}
				}
			}
		}
		sharedFilesMutex.RUnlock()
		
		sharedFilesMutex.Lock()
		for _, key := range keysToDelete {
			logEvent("SERVER_CLEANER", "RECORD_DELETE", fmt.Sprintf("Registro para '%s' eliminado. Nadie tiene una copia autoritativa.", key))
			delete(sharedFiles, key)
		}
		sharedFilesMutex.Unlock()
	}
}

func initServerData(selfAddr string) {
	sharedFiles["test_file.txt"] = DirectoryEntry{
		FileName: "test_file.txt",
		Size: 123,
		ModificationDate: time.Now(),
		Version: 1,
		TTL: 60,
		OwnerIP: selfAddr,
	}
	sharedFiles["perpetual_file.doc"] = DirectoryEntry{
		FileName: "perpetual_file.doc",
		Size: 456,
		ModificationDate: time.Now(),
		Version: 1,
		TTL: 0,
		OwnerIP: selfAddr,
	}
	logEvent("SERVER", "DIRECTORY_INIT", "Directorio inicializado con archivos de prueba.")
}

func main() {
    // Definimos un flag para el puerto, con un valor predeterminado de 8080
    port := flag.String("port", "8080", "Puerto para que el servidor escuche")
    flag.Parse() // Analiza los argumentos de la línea de comandos
    
    selfAddr := fmt.Sprintf("127.0.0.1:%s", *port)
    
	cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al cargar el par de claves: %v. Asegúrate de haber ejecutado 'setup_ca.go'.", err))
		panic(err)
	}
	caCert, err := os.ReadFile("ca.crt")
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al cargar certificado de la CA: %v", err))
		panic(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caCert) {
		logEvent("SERVER", "ERROR", "Falla al agregar certificado de la CA al pool.")
		panic("Falla al agregar certificado de la CA al pool.")
	}
	logEvent("SERVER", "CERT_LOADED", "Certificado y clave de servidor cargados.")

	dtlsConfig := &dtls.Config{
		Certificates:         []tls.Certificate{cert},
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}

    // Usamos el puerto del flag en el ResolveUDPAddr
    addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%s", *port))
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al resolver dirección: %v", err))
		panic(err)
	}

	gossipProtocol, err = NewGossipProtocol(knownPeers, dtlsConfig, selfAddr)
	if err != nil {
		panic(err)
	}

	go gossipProtocol.StartGossipRoutine()

	initServerData(selfAddr)
	go cleaner()

	listener, err := dtls.Listen("udp", addr, dtlsConfig)
	if err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al escuchar DTLS: %v", err))
		panic(err)
	}
	defer listener.Close()
	logEvent("SERVER", "LISTENING", fmt.Sprintf("Servidor DTLS escuchando en el puerto %s...", *port))

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
	go gossipProtocol.AddPeer(conn.RemoteAddr().String())

	conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

	buffer := make([]byte, 2048)
	n, err := conn.Read(buffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			logEvent("SERVER", "CONNECTION_CLOSED", fmt.Sprintf("Conexión con %s cerrada por inactividad: %v", conn.RemoteAddr(), err))
		} else {
			logEvent("SERVER", "CONNECTION_CLOSED", fmt.Sprintf("Conexión con %s cerrada por error de lectura: %v", conn.RemoteAddr(), err))
		}
		return
	}

	var msg NetworkMessage
	if err := json.Unmarshal(buffer[:n], &msg); err != nil {
		logEvent("SERVER", "ERROR", fmt.Sprintf("Mensaje malformado de %s: %v", conn.RemoteAddr(), err))
		return
	}

	if msg.Type == "HEARTBEAT" {
		go gossipProtocol.AddPeer(conn.RemoteAddr().String())
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
			payloadBytes, _ := json.Marshal(entry)
			responseMsg = NetworkMessage{
				Type:          "RESPONSE",
				Payload:       payloadBytes,
				Authoritative: true,
				SenderIP:      conn.LocalAddr().String(),
			}
		} else {
			responseMsg = NetworkMessage{
				Type:          "NACK",
				Payload:       []byte("Archivo no encontrado en el directorio."),
				Authoritative: false,
				SenderIP:      conn.LocalAddr().String(),
			}
		}

	case "GET_FULL_LIST":
		logEvent("SERVER", "QUERY_LIST", "Solicitud de lista completa")

		sharedFilesMutex.RLock()
		payloadBytes, _ := json.Marshal(sharedFiles)
		sharedFilesMutex.RUnlock()

		responseMsg = NetworkMessage{
			Type:          "RESPONSE_LIST",
			Payload:       payloadBytes,
			Authoritative: true,
			SenderIP:      conn.LocalAddr().String(),
		}

	case "REQUEST_STATUS":
		var fileName string
		json.Unmarshal(msg.Payload, &fileName)
		logEvent("SERVER", "STATUS_REQUEST", fmt.Sprintf("Petición de estado para '%s' de peer %s.", fileName, conn.RemoteAddr()))

		sharedFilesMutex.RLock()
		entry, found := sharedFiles[fileName]
		sharedFilesMutex.RUnlock()

		if found && entry.OwnerIP == conn.LocalAddr().String() {
			payloadBytes, _ := json.Marshal(entry)
			responseMsg = NetworkMessage{
				Type:          "STATUS_RESPONSE",
				Payload:       payloadBytes,
				Authoritative: true,
				SenderIP:      conn.LocalAddr().String(),
			}
		} else {
			responseMsg = NetworkMessage{
				Type:          "NACK",
				Payload:       []byte("No soy el dueño de este archivo."),
				Authoritative: false,
				SenderIP:      conn.LocalAddr().String(),
			}
		}

	case "GOSSIP_UPDATE":
		var entry DirectoryEntry
		json.Unmarshal(msg.Payload, &entry)
		sharedFilesMutex.Lock()
		sharedFiles[entry.FileName] = entry
		sharedFilesMutex.Unlock()
		logEvent("SERVER", "GOSSIP_UPDATE_RECEIVED", fmt.Sprintf("Recibida actualización de peer para '%s'.", entry.FileName))

	case "FILE_COPY_UPDATE":
		var updatedEntry DirectoryEntry
		json.Unmarshal(msg.Payload, &updatedEntry)
		logEvent("SERVER", "FILE_UPDATE", fmt.Sprintf("Recibida actualización para '%s'", updatedEntry.FileName))

		sharedFilesMutex.Lock()
		originalEntry, found := sharedFiles[updatedEntry.FileName]
		if !found || updatedEntry.Version > originalEntry.Version {
			sharedFiles[updatedEntry.FileName] = updatedEntry
			logEvent("SERVER", "UPDATE_SUCCESS", fmt.Sprintf("Archivo '%s' actualizado con éxito. Nuevo dueño: %s, Versión: %d", updatedEntry.FileName, updatedEntry.OwnerIP, updatedEntry.Version))
		} else {
			logEvent("SERVER", "UPDATE_REJECTED", fmt.Sprintf("Rechazada actualización de '%s'. La versión local es más reciente (%d) o igual (%d).", updatedEntry.FileName, originalEntry.Version, updatedEntry.Version))
		}
		sharedFilesMutex.Unlock()

		responseMsg = NetworkMessage{
			Type:          "UPDATE_ACK",
			Payload:       []byte("Actualización recibida y procesada."),
			Authoritative: true,
			SenderIP:      conn.LocalAddr().String(),
		}

	default:
		responseMsg = NetworkMessage{
			Type:          "NACK",
			Payload:       []byte("Tipo de petición no reconocido."),
			Authoritative: false,
			SenderIP:      conn.LocalAddr().String(),
		}
	}

	responseBytes, _ := json.Marshal(responseMsg)
	conn.Write(responseBytes)
	logEvent("SERVER", "MESSAGE_SENT", fmt.Sprintf("Respuesta enviada de tipo: %s", responseMsg.Type))
}
