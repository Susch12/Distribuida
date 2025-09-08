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
	RETRY_TIMEOUT = 500 * time.Millisecond
	INITIAL_WINDOW = 1 // Comienza con una ventana de tamaño 1 (Slow Start)
	SS_THRESHOLD = 16 // Umbral de inicio lento
)

// clientState manages the state for each client session.
type clientState struct {
	conn net.Conn
	clientID int
	remoteAddr string
	ackMu sync.Mutex
	lastAckReceived int
	packetsInWindow list.List
	packetTimers map[int]*time.Timer
	transferComplete bool
	// Control de Congestión
	windowSize int
	ssthresh int
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
	
	// Corregido: Codificar el certificado en formato PEM
	pem.Encode(certOut, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Certificate[0],
	})
	fmt.Println("[*] Certificado DTLS generado y guardado en cert.pem")

	// Configuración DTLS
	dtlsConfig := &dtls.Config{
		Certificates:         []tls.Certificate{certificate},
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
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
	fmt.Println("[+] Servidor DTLS-RUDP escuchando en el puerto", *port, "...")

	// Bucle principal para manejar nuevas conexiones
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("[!] Error al aceptar conexión DTLS:", err)
			continue
		}
		
		remoteAddr := conn.RemoteAddr().String()
		fmt.Printf("[+] Nuevo cliente DTLS conectado: %s\n", remoteAddr)

		// Usar Mutex para el mapa y evitar condiciones de carrera
		clientChannelsMutex.Lock()
		_, exists := clientChannels[remoteAddr]
		if !exists {
			contadorMutex.Lock()
			contadorClientes++
			clientID := contadorClientes
			contadorMutex.Unlock()

			fmt.Printf("[+] Iniciando manejo del cliente #%d (%s)...\n", clientID, remoteAddr)
			
			state := &clientState{
				conn: conn,
				clientID: clientID,
				remoteAddr: remoteAddr,
				packetTimers: make(map[int]*time.Timer),
				windowSize: INITIAL_WINDOW,
				ssthresh: SS_THRESHOLD,
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
		state.conn.Close()
		fmt.Printf("[*] Limpiado el estado para el cliente #%d (%s).\n", state.clientID, state.remoteAddr)
	}()

	// Ensure all timers are stopped when the function exits
	defer state.stopTimers() 

	fmt.Printf("[+] [Cliente #%d] Handshake DTLS completado.\n", state.clientID)

	// Start goroutine to receive ACKs
	go state.receiveACKs()

	// 2. Implementación de Ventana Deslizante para la Transferencia
	archivoAsignado := archivosParaClientes[(state.clientID-1)%len(archivosParaClientes)+1]
	fmt.Printf("[+] [Cliente #%d] Asignando archivo '%s'.\n", state.clientID, archivoAsignado)
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

	for state.packetsInWindow.Len() > 0 {
		fmt.Printf("[*] [Cliente #%d] Esperando ACKs, ventana con %d paquetes.\n", state.clientID, state.packetsInWindow.Len())
		time.Sleep(100 * time.Millisecond)
	}

	finPkt := Packet{Type: "FIN", Seq: nextSeq}
	state.sendPacket(finPkt)
	fmt.Printf("[+] [Cliente #%d] Enviado FIN. Esperando FIN-ACK.\n", state.clientID)
}

// sendPacketAndSetTimer sends a packet and starts its retransmission timer.
func (s *clientState) sendPacketAndSetTimer(pkt Packet) {
	s.sendPacket(pkt)
	s.packetsInWindow.PushBack(pkt)
	fmt.Printf("[+] [Cliente #%d] Enviado paquete Seq#%d\n", s.clientID, pkt.Seq)

	s.packetTimers[pkt.Seq] = time.AfterFunc(RETRY_TIMEOUT, func() {
		fmt.Printf("[!] [Cliente #%d] Temporizador expirado para Seq#%d. Retransmitiendo.\n", s.clientID, pkt.Seq)
		
		// Control de Congestión: Si hay retransmisión, reducimos la ventana
		s.ackMu.Lock()
		s.ssthresh = s.windowSize / 2
		if s.ssthresh < 2 {
			s.ssthresh = 2
		}
		s.windowSize = INITIAL_WINDOW
		s.ackMu.Unlock()
		
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
				if s.windowSize < s.ssthresh {
					// Slow Start: aumenta exponencialmente
					s.windowSize++
				} else {
					// Congestion Avoidance: aumenta linealmente
					// Nota: para un solo ACK, esto no se aplica directamente, pero es el concepto
				}
				fmt.Printf("[*] [Cliente #%d] Recibido ACK para Seq#%d. Tamaño de ventana: %d\n", s.clientID, pkt.Seq, s.windowSize)
				
				// Selective Repeat: Detener el temporizador del paquete específico
				if timer, ok := s.packetTimers[pkt.Seq]; ok {
					timer.Stop()
					delete(s.packetTimers, pkt.Seq)
					s.lastAckReceived = pkt.Seq // Asegurarse de que el último ACK recibido sea el más alto
				}
				
				// La ventana ahora se desliza en base a la lógica de Go-Back-N (el front)
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
		if pkt.Type == "FIN-ACK" {
			s.transferComplete = true
			return
		}
	}
}

func crearArchivosDeEjemplo() {
	for i := 1; i <= 3; i++ {
		nombreArchivo := fmt.Sprintf("archivo%d.txt", i)
		contenido := []string{
			fmt.Sprintf("Este es el contenido único del archivo %d.", i),
			"Línea de prueba A.",
			"Línea de prueba B.",
			"Fin del archivo.",
		}
		os.WriteFile(nombreArchivo, []byte(strings.Join(contenido, "\n")), 0644)
	}
	fmt.Println("[*] Archivos de ejemplo creados: archivo1.txt, archivo2.txt, archivo3.txt")
}
