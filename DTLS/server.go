package main

import (
	"bufio"
	"container/list"
	"crypto/tls"
	"encoding/json"
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

// Packet define nuestra estructura de paquete RUDP
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
	WINDOW_SIZE   = 10 // Tamaño de la ventana deslizante
)

// clientState manages the state for each client session.
type clientState struct {
	conn       net.Conn
	clientID   int
	remoteAddr string
	ackMu      sync.Mutex
	lastAckReceived int
	packetsInWindow list.List
	packetTimers map[int]*time.Timer
	transferComplete bool
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

	base := 100 // Número de secuencia del primer paquete
	nextSeq := base
	
	scanner := bufio.NewScanner(file)
	for !state.transferComplete {
		// Enviar paquetes hasta que la ventana esté llena
		if state.packetsInWindow.Len() < WINDOW_SIZE && scanner.Scan() {
			line := scanner.Text()
			dataPkt := Packet{Type: "DATA", Seq: nextSeq, Payload: []byte(line)}
			state.sendPacketAndSetTimer(dataPkt)
			nextSeq++
		}
		if state.packetsInWindow.Len() == 0 && !scanner.Scan() {
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

	// Temporizador por paquete
	s.packetTimers[pkt.Seq] = time.AfterFunc(RETRY_TIMEOUT, func() {
		fmt.Printf("[!] [Cliente #%d] Temporizador expirado para Seq#%d. Retransmitiendo.\n", s.clientID, pkt.Seq)
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
			fmt.Printf("[!] [Cliente #%d] Error en lectura DTLS: %s\n", s.clientID, err)
			s.transferComplete = true
			return
		}
		var pkt Packet
		if err := json.Unmarshal(buffer[:n], &pkt); err != nil {
			fmt.Printf("[!] [Cliente #%d] Paquete malformado: %s\n", s.clientID, err)
			continue
		}

		if pkt.Type == "ACK" {
			s.ackMu.Lock()
			if pkt.Seq > s.lastAckReceived {
				s.lastAckReceived = pkt.Seq
				fmt.Printf("[*] [Cliente #%d] Recibido ACK acumulativo para Seq#%d\n", s.clientID, pkt.Seq)
				
				// Deslizar la ventana y detener temporizadores
				for e := s.packetsInWindow.Front(); e != nil; {
					p := e.Value.(Packet)
					if p.Seq <= pkt.Seq {
						if timer, ok := s.packetTimers[p.Seq]; ok {
							timer.Stop()
							delete(s.packetTimers, p.Seq)
						}
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
			fmt.Printf("[*] [Cliente #%d] Recibido FIN-ACK.\n", s.clientID)
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
