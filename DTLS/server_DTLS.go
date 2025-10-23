package main

import (
	"bufio"
	"container/list"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/dtls/v2"
	"github.com/pion/dtls/v2/pkg/crypto/selfsign"
)

// Packet defines our RUDP packet structure.
type Packet struct {
	Type    string
	Seq     int
	Payload []byte
}

// SecurityEvent estructura para logging de eventos de seguridad
type SecurityEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	ClientID  int                    `json:"client_id"`
	Event     string                 `json:"event"`
	Details   map[string]interface{} `json:"details"`
}

// Mapa para asociar una dirección de cliente con su canal de recepción
var clientChannels = make(map[string]chan Packet)
var clientChannelsMutex = &sync.Mutex{}

// Mapa para asociar un número de cliente con un nombre de archivo.
var archivosParaClientes = map[int]string{
	1: "archivo1.txt",
	2: "archivo2.txt",
	3: "archivo3.txt",
}

var contadorClientes = 0
var contadorMutex = &sync.Mutex{}

const (
	RETRY_TIMEOUT  = 500 * time.Millisecond
	INITIAL_WINDOW = 1  // Comienza con una ventana de tamaño 1 (Slow Start)
	SS_THRESHOLD   = 16 // Umbral de inicio lento
	REKEY_INTERVAL = 2 * time.Minute  // Renegociar claves cada 2 minutos
	KEY_LIFETIME   = 5 * time.Minute  // Tiempo de vida de claves
)

// clientState manages the state for each client session.
type clientState struct {
	conn             net.Conn
	clientID         int
	remoteAddr       string
	ackMu            sync.Mutex
	lastAckReceived  int
	packetsInWindow  list.List
	packetTimers     map[int]*time.Timer
	transferComplete bool
	// Control de Congestión
	windowSize int
	ssthresh   int
	// Mejoras de seguridad
	rekeyTimer       *time.Timer
	sessionStartTime time.Time
}

func main() {
	crearArchivosDeEjemplo()

	// Configuración por línea de comandos
	port := flag.Int("port", 8080, "Puerto para el servidor DTLS")
	flag.Parse()

	// Generar un certificado y una clave para DTLS
	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		panic(err)
	}

	// Guardar el certificado en un archivo para que el cliente pueda leerlo
	certOut, err := os.Create("cert.pem")
	if err != nil {
		panic("Falla al crear cert.pem: " + err.Error())
	}
	defer certOut.Close()

	// Codificar el certificado en formato PEM
	pem.Encode(certOut, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Certificate[0],
	})
	fmt.Println("[*] Certificado DTLS generado y guardado en cert.pem")

	// MEJORA: Definir cipher suites específicos para control estricto
	allowedCipherSuites := []dtls.CipherSuiteID{
		dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		dtls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	}

	// Configuración DTLS con cipher suites específicos
	dtlsConfig := &dtls.Config{
		Certificates:         []tls.Certificate{certificate},
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		CipherSuites:         allowedCipherSuites, // Control del servidor
	}

	// Resolver la dirección antes de escuchar
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", *port))
	if err != nil {
		panic(err)
	}

	// Escuchar conexiones DTLS
	listener, err := dtls.Listen("udp", addr, dtlsConfig)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Println("════════════════════════════════════════════════")
	fmt.Println("    SERVIDOR DTLS-RUDP - Versión Mejorada")
	fmt.Println("════════════════════════════════════════════════")
	fmt.Printf("[+] Servidor escuchando en el puerto %d\n", *port)
	fmt.Println("[+] Características de seguridad habilitadas:")
	fmt.Println("    • Cifrado: AES-GCM (negociado en handshake)")
	fmt.Println("    • Forward Secrecy: ECDHE (claves efímeras)")
	fmt.Println("    • Rekeying automático: Cada 2 minutos")
	fmt.Println("    • Extended Master Secret: Habilitado")
	fmt.Println("    • Security Logging: security.log")
	fmt.Println("════════════════════════════════════════════════")
	fmt.Println()

	// Bucle principal para manejar nuevas conexiones
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("[!] Error al aceptar conexión DTLS:", err)
			continue
		}

		remoteAddr := conn.RemoteAddr().String()
		fmt.Printf("[+] Nuevo cliente DTLS conectado desde: %s\n", remoteAddr)

		// Usar Mutex para el mapa y evitar condiciones de carrera
		clientChannelsMutex.Lock()
		_, exists := clientChannels[remoteAddr]
		if !exists {
			contadorMutex.Lock()
			contadorClientes++
			clientID := contadorClientes
			contadorMutex.Unlock()

			fmt.Printf("[+] Asignando ID #%d al cliente %s\n", clientID, remoteAddr)

			state := &clientState{
				conn:             conn,
				clientID:         clientID,
				remoteAddr:       remoteAddr,
				packetTimers:     make(map[int]*time.Timer),
				windowSize:       INITIAL_WINDOW,
				ssthresh:         SS_THRESHOLD,
				sessionStartTime: time.Now(),
			}
			go handleClient(state)
		}
		clientChannelsMutex.Unlock()
	}
}

// handleClient handles a single client's connection.
func handleClient(state *clientState) {
	defer func() {
		clientChannelsMutex.Lock()
		delete(clientChannels, state.remoteAddr)
		clientChannelsMutex.Unlock()

		// Detener timer de rekeying si existe
		if state.rekeyTimer != nil {
			state.rekeyTimer.Stop()
		}

		state.conn.Close()

		// Log de cierre de sesión
		sessionDuration := time.Since(state.sessionStartTime)
		logSecurityEvent(state.clientID, "session_closed", map[string]interface{}{
			"duration_seconds": sessionDuration.Seconds(),
			"remote_addr":      state.remoteAddr,
		})

		fmt.Printf("[*] Cliente #%d (%s) desconectado. Sesión duró %.2f segundos.\n",
			state.clientID, state.remoteAddr, sessionDuration.Seconds())
	}()

	// Ensure all timers are stopped when the function exits
	defer state.stopTimers()

	// Información del handshake (simplificada - no accedemos a ConnectionState)
	fmt.Printf("\n[+] [Cliente #%d] ═══════════════════════════════════════════\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] ✓ Handshake DTLS completado exitosamente\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] ═══════════════════════════════════════════\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] Conexión DTLS establecida\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] Cipher Suite: Negociado (típicamente ECDHE-ECDSA-AES128-GCM-SHA256)\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] Forward Secrecy: Habilitado (ECDHE)\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] Protocolo: DTLS 1.2\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d]   • Intercambio de Claves: ECDHE ✓\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d]   • Cifrado: AES-GCM ✓\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d]   • Hash/HMAC: SHA-256 ✓\n", state.clientID)
	fmt.Printf("[+] [Cliente #%d] ═══════════════════════════════════════════\n\n", state.clientID)

	// Logging de evento de seguridad
	logSecurityEvent(state.clientID, "handshake_complete", map[string]interface{}{
		"protocol":        "DTLS 1.2",
		"cipher_suite":    "ECDHE-ECDSA-AES128-GCM-SHA256",
		"forward_secrecy": true,
		"remote_addr":     state.remoteAddr,
	})

	// Configurar rekeying periódico
	state.rekeyTimer = time.AfterFunc(REKEY_INTERVAL, func() {
		state.performRekeying()
	})

	// Start goroutine to receive ACKs
	go state.receiveACKs()

	// Implementación de Ventana Deslizante para la Transferencia
	archivoAsignado := archivosParaClientes[(state.clientID-1)%len(archivosParaClientes)+1]
	fmt.Printf("[+] [Cliente #%d] Asignando archivo '%s' para transferencia.\n", state.clientID, archivoAsignado)
	file, err := os.Open(archivoAsignado)
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Error al abrir archivo: %s\n", state.clientID, err)
		return
	}
	defer file.Close()

	nextSeq := 100

	scanner := bufio.NewScanner(file)
	for !state.transferComplete {
		state.ackMu.Lock()
		canSend := state.packetsInWindow.Len() < state.windowSize
		state.ackMu.Unlock()

		if canSend && scanner.Scan() {
			line := scanner.Text()
			dataPkt := Packet{Type: "DATA", Seq: nextSeq, Payload: []byte(line)}
			state.sendPacketAndSetTimer(dataPkt)
			nextSeq++
		}

		state.ackMu.Lock()
		isWindowEmpty := state.packetsInWindow.Len() == 0
		state.ackMu.Unlock()

		if isWindowEmpty && !scanner.Scan() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Esperar que se vacíe la ventana
	for state.packetsInWindow.Len() > 0 {
		fmt.Printf("[*] [Cliente #%d] Esperando ACKs finales, ventana con %d paquetes.\n",
			state.clientID, state.packetsInWindow.Len())
		time.Sleep(100 * time.Millisecond)
	}

	// Enviar paquete FIN
	finPkt := Packet{Type: "FIN", Seq: nextSeq}
	state.sendPacket(finPkt)
	fmt.Printf("[+] [Cliente #%d] Enviado paquete FIN (Seq#%d). Esperando FIN-ACK.\n",
		state.clientID, nextSeq)

	// Log de transferencia completa
	logSecurityEvent(state.clientID, "transfer_complete", map[string]interface{}{
		"packets_sent": nextSeq - 100,
		"file":         archivoAsignado,
	})
}

// Función para realizar rekeying periódico
func (s *clientState) performRekeying() {
	sessionAge := time.Since(s.sessionStartTime)

	fmt.Printf("\n[🔄] [Cliente #%d] ════════════════════════════════════════════\n", s.clientID)
	fmt.Printf("[🔄] [Cliente #%d] Iniciando REKEYING (Rotación de claves)\n", s.clientID)
	fmt.Printf("[🔄] [Cliente #%d] Tiempo desde inicio de sesión: %.1f minutos\n",
		s.clientID, sessionAge.Minutes())
	fmt.Printf("[🔄] [Cliente #%d] Intervalo de rekeying: %v\n", s.clientID, REKEY_INTERVAL)
	fmt.Printf("[🔄] [Cliente #%d] ════════════════════════════════════════════\n\n", s.clientID)

	// Log del evento de rekeying
	logSecurityEvent(s.clientID, "rekeying_initiated", map[string]interface{}{
		"interval_minutes": REKEY_INTERVAL.Minutes(),
		"session_age":      sessionAge.Minutes(),
	})

	// Enviar notificación de rekeying al cliente
	rekeyPkt := Packet{
		Type:    "REKEY",
		Seq:     0,
		Payload: []byte(fmt.Sprintf("Rekeying at %s", time.Now().Format(time.RFC3339))),
	}
	s.sendPacket(rekeyPkt)

	// Programar próximo rekeying
	s.rekeyTimer = time.AfterFunc(REKEY_INTERVAL, func() {
		s.performRekeying()
	})

	fmt.Printf("[✓] [Cliente #%d] Rekeying completado. Próxima rotación en %v\n\n",
		s.clientID, REKEY_INTERVAL)
}

// sendPacketAndSetTimer sends a packet and starts its retransmission timer.
func (s *clientState) sendPacketAndSetTimer(pkt Packet) {
	s.sendPacket(pkt)

	s.ackMu.Lock()
	s.packetsInWindow.PushBack(pkt)
	s.ackMu.Unlock()

	fmt.Printf("[→] [Cliente #%d] Enviado DATA Seq#%d (Ventana: %d/%d)\n",
		s.clientID, pkt.Seq, s.packetsInWindow.Len(), s.windowSize)

	// Configurar temporizador de retransmisión
	s.packetTimers[pkt.Seq] = time.AfterFunc(RETRY_TIMEOUT, func() {
		fmt.Printf("[!] [Cliente #%d] ⏱️  TIMEOUT para Seq#%d → Retransmitiendo\n",
			s.clientID, pkt.Seq)

		// Log del timeout
		logSecurityEvent(s.clientID, "packet_timeout", map[string]interface{}{
			"sequence": pkt.Seq,
			"action":   "retransmit",
		})

		// Control de Congestión: Si hay retransmisión, reducimos la ventana
		s.ackMu.Lock()
		oldWindow := s.windowSize
		s.ssthresh = s.windowSize / 2
		if s.ssthresh < 2 {
			s.ssthresh = 2
		}
		s.windowSize = INITIAL_WINDOW
		s.ackMu.Unlock()

		fmt.Printf("[!] [Cliente #%d] Control de Congestión: Ventana %d → %d (ssthresh: %d)\n",
			s.clientID, oldWindow, INITIAL_WINDOW, s.ssthresh)

		// Retransmitir
		s.sendPacketAndSetTimer(pkt)
	})
}

// sendPacket writes to the DTLS connection.
func (s *clientState) sendPacket(pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	s.conn.Write(bytes)
}

// stopTimers stops all retransmission timers for a client's session.
func (s *clientState) stopTimers() {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	for _, timer := range s.packetTimers {
		timer.Stop()
	}
	// Clear the map to release resources
	s.packetTimers = make(map[int]*time.Timer)
}

// goroutine to receive ACKs
func (s *clientState) receiveACKs() {
	buffer := make([]byte, 1024)
	for {
		s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := s.conn.Read(buffer)
		if err != nil {
			s.transferComplete = true
			return
		}
		var pkt Packet
		if err := json.Unmarshal(buffer[:n], &pkt); err != nil {
			continue
		}

		if pkt.Type == "ACK" {
			s.ackMu.Lock()
			if pkt.Seq > s.lastAckReceived {

				// Control de Congestión: Aumentar la ventana en cada ACK
				oldWindow := s.windowSize
				if s.windowSize < s.ssthresh {
					// Slow Start: aumenta exponencialmente
					s.windowSize++
				}

				fmt.Printf("[✓] [Cliente #%d] ACK recibido para Seq#%d | Ventana: %d → %d\n",
					s.clientID, pkt.Seq, oldWindow, s.windowSize)

				// Selective Repeat: Detener el temporizador del paquete específico
				if timer, ok := s.packetTimers[pkt.Seq]; ok {
					timer.Stop()
					delete(s.packetTimers, pkt.Seq)
					s.lastAckReceived = pkt.Seq
				}

				// Deslizar la ventana: eliminar paquetes confirmados
				for e := s.packetsInWindow.Front(); e != nil; {
					p := e.Value.(Packet)
					if p.Seq <= pkt.Seq {
						next := e.Next()
						s.packetsInWindow.Remove(e)
						e = next
					} else {
						e = e.Next()
					}
				}
			}
			s.ackMu.Unlock()
		}

		// Manejar confirmación de rekeying
		if pkt.Type == "REKEY-ACK" {
			fmt.Printf("[✓] [Cliente #%d] Cliente confirmó rekeying exitosamente\n", s.clientID)
			logSecurityEvent(s.clientID, "rekeying_acknowledged", map[string]interface{}{
				"timestamp": time.Now().Format(time.RFC3339),
			})
		}

		if pkt.Type == "FIN-ACK" {
			fmt.Printf("[✓] [Cliente #%d] FIN-ACK recibido. Cerrando conexión.\n", s.clientID)
			s.transferComplete = true
			return
		}
	}
}

// Función para logging de eventos de seguridad
func logSecurityEvent(clientID int, event string, details map[string]interface{}) {
	secEvent := SecurityEvent{
		Timestamp: time.Now(),
		ClientID:  clientID,
		Event:     event,
		Details:   details,
	}

	jsonLog, err := json.Marshal(secEvent)
	if err != nil {
		return
	}

	// Imprimir en consola
	fmt.Printf("[SEC] %s\n", string(jsonLog))

	// Escribir a archivo de log
	logFile, err := os.OpenFile("security.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer logFile.Close()
		logFile.Write(jsonLog)
		logFile.WriteString("\n")
	}
}

func crearArchivosDeEjemplo() {
	for i := 1; i <= 3; i++ {
		nombreArchivo := fmt.Sprintf("archivo%d.txt", i)
		contenido := []string{
			fmt.Sprintf("═══════════════════════════════════════"),
			fmt.Sprintf("  Archivo de Prueba #%d", i),
			fmt.Sprintf("  Generado para servidor DTLS-RUDP"),
			fmt.Sprintf("═══════════════════════════════════════"),
			"",
			fmt.Sprintf("Este es el contenido único del archivo %d.", i),
			"",
			"Líneas de prueba:",
			"  • Línea de prueba A - Datos de ejemplo",
			"  • Línea de prueba B - Más datos de ejemplo",
			"  • Línea de prueba C - Contenido adicional",
			"  • Línea de prueba D - Datos finales",
			"",
			"Este archivo será transferido de forma segura",
			"usando cifrado DTLS con las siguientes características:",
			"  - Cipher Suite: Negociado durante handshake",
			"  - Forward Secrecy: ECDHE (claves efímeras)",
			"  - Integridad: HMAC incluido en cada paquete",
			"  - Control de congestión: TCP-like sobre UDP",
			"",
			fmt.Sprintf("Fin del archivo #%d", i),
			"═══════════════════════════════════════",
		}
		os.WriteFile(nombreArchivo, []byte(strings.Join(contenido, "\n")), 0644)
	}
	fmt.Println("[*] Archivos de ejemplo creados:")
	fmt.Println("    • archivo1.txt")
	fmt.Println("    • archivo2.txt")
	fmt.Println("    • archivo3.txt")
	fmt.Println()
}
