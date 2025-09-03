package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Packet define nuestra estructura de paquete RUDP
type Packet struct {
	Type    string
	Seq     int
	Payload []byte
}

// Canal para la comunicación con una goroutine específica
type clientChannel chan Packet

// Mapa para asociar una dirección de cliente con su canal de recepción
var clientChannels = make(map[string]clientChannel)
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
	MAX_RETRIES   = 5
)

func main() {
	crearArchivosDeEjemplo()

	addr, err := net.ResolveUDPAddr("udp", ":8080")
	if err != nil {
		panic(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	fmt.Println("[+] Servidor RUDP escuchando en el puerto 8080...")

	// Goroutine para leer paquetes y distribuirlos
	go packetDistributor(conn)

	// Bucle principal para manejar nuevas conexiones (SYN)
	buffer := make([]byte, 1024)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("[!] Error al leer de UDP:", err)
			continue
		}

		var pkt Packet
		if err := json.Unmarshal(buffer[:n], &pkt); err != nil {
			fmt.Println("[!] Error al decodificar paquete:", err)
			continue
		}

		if pkt.Type == "SYN" {
			clientChannelsMutex.Lock()
			if _, exists := clientChannels[clientAddr.String()]; !exists {
				// Crear un canal para el nuevo cliente
				clientCh := make(clientChannel)
				clientChannels[clientAddr.String()] = clientCh
				clientChannelsMutex.Unlock()

				contadorMutex.Lock()
				contadorClientes++
				clientID := contadorClientes
				contadorMutex.Unlock()

				fmt.Printf("[+] Recibido SYN del nuevo cliente #%d (%s). Iniciando...\n", clientID, clientAddr.String())
				go handleClient(conn, clientAddr, clientID, clientCh)
			} else {
				clientChannelsMutex.Unlock()
			}
		}
	}
}

// packetDistributor lee del socket y envía los paquetes a los canales de los clientes
func packetDistributor(conn *net.UDPConn) {
	buffer := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("[!] Error en el distribuidor de paquetes:", err)
			continue
		}

		var pkt Packet
		if err := json.Unmarshal(buffer[:n], &pkt); err != nil {
			fmt.Println("[!] Paquete malformado:", err)
			continue
		}

		clientChannelsMutex.Lock()
		clientCh, ok := clientChannels[addr.String()]
		clientChannelsMutex.Unlock()

		if ok {
			// Enviar el paquete al canal del cliente correspondiente
			// Usamos un select para evitar el bloqueo si la goroutine del cliente no está lista para leer
			select {
			case clientCh <- pkt:
				// Paquete enviado
			default:
				// El canal está lleno, se omite el paquete para no bloquear
				fmt.Println("[!] Advertencia: canal de cliente lleno para", addr.String())
			}
		}
	}
}

func handleClient(conn *net.UDPConn, addr *net.UDPAddr, clientID int, clientCh clientChannel) {
	defer func() {
		clientChannelsMutex.Lock()
		delete(clientChannels, addr.String())
		close(clientCh)
		clientChannelsMutex.Unlock()
		fmt.Printf("[*] Limpiado el estado para el cliente #%d (%s).\n", clientID, addr.String())
	}()

	// --- 1. Realizar el Handshake ---
	synAckPkt := Packet{Type: "SYN-ACK", Seq: 1}
	sendPacket(conn, addr, synAckPkt)

	// Esperamos el ACK del cliente desde el canal
	select {
	case pkt := <-clientCh:
		if pkt.Type != "ACK" || pkt.Seq != 1 {
			fmt.Printf("[!] [Cliente #%d] Handshake fallido: paquete inesperado\n", clientID)
			return
		}
	case <-time.After(RETRY_TIMEOUT):
		fmt.Printf("[!] [Cliente #%d] Handshake fallido: timeout al esperar ACK\n", clientID)
		return
	}
	fmt.Printf("[+] [Cliente #%d] Handshake completado.\n", clientID)

	// --- 2. Enviar el archivo dinámicamente ---
	archivoAsignado := archivosParaClientes[(clientID-1)%len(archivosParaClientes)+1]
	fmt.Printf("[+] [Cliente #%d] Asignando archivo '%s'.\n", clientID, archivoAsignado)

	file, err := os.Open(archivoAsignado)
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Error al abrir archivo: %s\n", clientID, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	seqNum := 100
	transferSuccess := true
	for scanner.Scan() {
		line := scanner.Text()
		dataPkt := Packet{Type: "DATA", Seq: seqNum, Payload: []byte(line)}

		ackReceived := false
		for i := 0; i < MAX_RETRIES; i++ {
			sendPacket(conn, addr, dataPkt)
			select {
			case pkt := <-clientCh:
				if pkt.Type == "ACK" && pkt.Seq == seqNum {
					ackReceived = true
					break
				}
			case <-time.After(RETRY_TIMEOUT):
				// Timeout, reintentar
			}
			if ackReceived {
				break
			}
		}

		if !ackReceived {
			fmt.Printf("[!] [Cliente #%d] Falla al enviar paquete Seq#%d. Abortando.\n", clientID, seqNum)
			transferSuccess = false
			break // Salir del bucle de escaneo
		}
		seqNum++
	}

	// --- 3. Terminar la conexión ---
	if transferSuccess {
		finPkt := Packet{Type: "FIN", Seq: seqNum}
		sendPacket(conn, addr, finPkt)
		fmt.Printf("[+] [Cliente #%d] Enviado FIN. Conexión cerrada.\n", clientID)
	} else {
		// En este caso, no enviamos FIN. Simplemente cerramos la goroutine.
		fmt.Printf("[!] [Cliente #%d] No se pudo completar la transferencia. El cliente puede tener un archivo incompleto.\n", clientID)
	}
}

// sendPacket envía un paquete a una dirección específica
func sendPacket(conn *net.UDPConn, addr *net.UDPAddr, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.WriteToUDP(bytes, addr)
}

// Función para crear archivos de texto de ejemplo
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
