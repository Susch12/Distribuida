package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
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
	gossipProtocol   *GossipProtocol
	selfAddr         string // Variable global para la dirección del servidor
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

func initServerData() {
	sharedFiles["test_file.txt"] = DirectoryEntry{
		FileName:         "test_file.txt",
		Size:             123,
		ModificationDate: time.Now(),
		Version:          1,
		TTL:              60,
		OwnerIP:          selfAddr, // Usa la dirección del servidor
	}
	sharedFiles["perpetual_file.doc"] = DirectoryEntry{
		FileName:         "perpetual_file.doc",
		Size:             456,
		ModificationDate: time.Now(),
		Version:          1,
		TTL:              0,
		OwnerIP:          selfAddr, // Usa la dirección del servidor
	}
	logEvent("SERVER", "DIRECTORY_INIT", "Directorio inicializado con archivos de prueba.")
}

func main() {
	port := flag.String("port", "8080", "Puerto para que el servidor escuche")
	peersStr := flag.String("peers", "", "Lista de peers iniciales, separados por comas (ej: localhost:8081,localhost:8082)")
	flag.Parse()

	selfAddr = fmt.Sprintf("127.0.0.1:%s", *port) // Se inicializa la variable global
	var knownPeers []string
	if *peersStr != "" {
		knownPeers = strings.Split(*peersStr, ",")
	}

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

	initServerData()
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
	clientAddr := conn.RemoteAddr().String()
	logEvent("SERVER", "NEW_CONNECTION", fmt.Sprintf("Conexión aceptada de %s", clientAddr))

	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		buffer := make([]byte, 2048)
		n, err := conn.Read(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logEvent("SERVER", "CONNECTION_CLOSED", fmt.Sprintf("Conexión con %s cerrada por inactividad: %v", clientAddr, err))
			} else {
				logEvent("SERVER", "CONNECTION_CLOSED", fmt.Sprintf("Conexión con %s cerrada por error de lectura: %v", clientAddr, err))
			}
			conn.Close() // Cierra la conexión solo cuando hay un error
			return // Sale del bucle y de la goroutine
		}

		var msg NetworkMessage
		if err := json.Unmarshal(buffer[:n], &msg); err != nil {
			logEvent("SERVER", "ERROR", fmt.Sprintf("Mensaje malformado de %s: %v", clientAddr, err))
			conn.Close() // Cierra la conexión si el mensaje es inválido
			return
		}

		logEvent("SERVER", "MESSAGE_RECEIVED", fmt.Sprintf("De %s, tipo: %s", clientAddr, msg.Type))

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
		case "ADD_FILE":
			var fileName string
			json.Unmarshal(msg.Payload, &fileName)
			logEvent("SERVER", "ADD_FILE_REQUEST", fmt.Sprintf("Petición para agregar el archivo '%s'.", fileName))
			sharedFilesMutex.Lock()
			file, err := os.Create(fileName)
			if err != nil {
				sharedFilesMutex.Unlock()
				logEvent("SERVER", "ERROR", fmt.Sprintf("Falla al crear el archivo '%s': %v", fileName, err))
				responseMsg = NetworkMessage{
					Type:    "NACK",
					Payload: []byte("Error al crear el archivo."),
				}
			} else {
				file.Close()
				newEntry := DirectoryEntry{
					FileName:         fileName,
					Size:             0,
					ModificationDate: time.Now(),
					Version:          1,
					TTL:              3600,
					OwnerIP:          conn.LocalAddr().String(),
				}
				sharedFiles[fileName] = newEntry
				sharedFilesMutex.Unlock()
				logEvent("SERVER", "NEW_FILE_ADDED", fmt.Sprintf("Nuevo archivo '%s' agregado a la lista local.", fileName))
				go gossipProtocol.GossipUpdateAllPeers(newEntry)
				responseMsg = NetworkMessage{
					Type:          "UPDATE_ACK",
					Payload:       []byte("Archivo agregado y compartido."),
					Authoritative: true,
					SenderIP:      conn.LocalAddr().String(),
				}
			}
		case "REQUEST_FILE":
			var fileName string
			json.Unmarshal(msg.Payload, &fileName)
			logEvent("SERVER", "FILE_REQUEST", fmt.Sprintf("Solicitud de archivo '%s' recibida.", fileName))
			sharedFilesMutex.RLock()
			entry, found := sharedFiles[fileName]
			sharedFilesMutex.RUnlock()
			if found && entry.OwnerIP == conn.LocalAddr().String() {
				fileContent, err := os.ReadFile(fileName)
				if err != nil {
					logEvent("SERVER", "FILE_ERROR", fmt.Sprintf("Falla al leer el archivo '%s': %v", fileName, err))
					responseMsg = NetworkMessage{
						Type: "NACK",
						Payload: []byte("Error al leer el archivo."),
					}
				} else {
					responseMsg = NetworkMessage{
						Type:    "FILE_RESPONSE",
						Payload: fileContent,
					}
					logEvent("SERVER", "FILE_SENT", fmt.Sprintf("Archivo '%s' enviado a %s.", fileName, conn.RemoteAddr()))
				}
			} else if found {
				responseMsg = NetworkMessage{
					Type:    "REDIRECT_OWNER",
					Payload: []byte(entry.OwnerIP),
				}
				logEvent("SERVER", "REDIRECT", fmt.Sprintf("Redireccionando solicitud de '%s' a %s.", fileName, entry.OwnerIP))
			} else {
				responseMsg = NetworkMessage{
					Type:          "NACK",
					Payload:       []byte("Archivo no encontrado en el directorio."),
					Authoritative: false,
					SenderIP:      conn.LocalAddr().String(),
				}
			}
		case "FILE_WRITE_UPDATE":
			var fileUpdate FileUpdate
			json.Unmarshal(msg.Payload, &fileUpdate)
			logEvent("SERVER", "FILE_WRITE_UPDATE", fmt.Sprintf("Recibida actualización para '%s' desde %s.", fileUpdate.FileName, conn.RemoteAddr()))
			sharedFilesMutex.Lock()
			entry, found := sharedFiles[fileUpdate.FileName]
			if !found || fileUpdate.Version < entry.Version {
				sharedFilesMutex.Unlock()
				responseMsg = NetworkMessage{
					Type:          "UPDATE_REJECTED",
					Payload:       []byte("Actualización rechazada: la versión local es más reciente."),
					Authoritative: true,
					SenderIP:      conn.LocalAddr().String(),
				}
				logEvent("SERVER", "UPDATE_REJECTED", fmt.Sprintf("Rechazada actualización de '%s'. La versión del cliente (%d) es más antigua que la local (%d).", fileUpdate.FileName, fileUpdate.Version, entry.Version))
			} else {
				if entry.ModificationDate.After(fileUpdate.ModificationDate) {
					sharedFilesMutex.Unlock()
					logEvent("SERVER", "COLLISION_DETECTED", fmt.Sprintf("Colisión en '%s'. La versión del servidor (%v) es más reciente que la del cliente (%v).", fileUpdate.FileName, entry.ModificationDate, fileUpdate.ModificationDate))
					responseMsg = NetworkMessage{
						Type:          "UPDATE_REJECTED",
						Payload:       []byte("Actualización rechazada por colisión. La versión del servidor es más reciente."),
						Authoritative: true,
						SenderIP:      conn.LocalAddr().String(),
					}
				} else {
					err := os.WriteFile(fileUpdate.FileName, fileUpdate.Content, 0644)
					if err != nil {
						sharedFilesMutex.Unlock()
						logEvent("SERVER", "FILE_ERROR", fmt.Sprintf("Falla al escribir en el archivo '%s': %v", fileUpdate.FileName, err))
						responseMsg = NetworkMessage{
							Type:    "NACK",
							Payload: []byte("Error al escribir el archivo."),
						}
					} else {
						entry.Version = fileUpdate.Version + 1
						entry.Size = int64(len(fileUpdate.Content))
						entry.ModificationDate = time.Now()
						sharedFiles[fileUpdate.FileName] = entry
						sharedFilesMutex.Unlock()
						responseMsg = NetworkMessage{
							Type:          "UPDATE_ACK",
							Payload:       []byte("Archivo actualizado con éxito."),
							Authoritative: true,
							SenderIP:      conn.LocalAddr().String(),
						}
						logEvent("SERVER", "UPDATE_SUCCESS", fmt.Sprintf("Archivo '%s' actualizado con éxito. Nueva versión: %d", fileUpdate.FileName, entry.Version))
					}
				}
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
}
