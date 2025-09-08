package main

import (
	"bufio"
	"container/list"
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
	WINDOW_SIZE   = 10 // Tamaño de la ventana deslizante
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

	// Bucle principal que maneja todos los paquetes entrantes
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

		// Bloqueo para acceder al mapa de canales de clientes
		clientChannelsMutex.Lock()
		clientCh, exists := clientChannels[clientAddr.String()]
		clientChannelsMutex.Unlock()

		if !exists {
			// Es un paquete de un cliente nuevo. Solo procesar si es SYN.
			if pkt.Type == "SYN" {
				clientChannelsMutex.Lock()
				// Se crea el canal del cliente con búfer
				clientCh := make(clientChannel, WINDOW_SIZE*2)
				clientChannels[clientAddr.String()] = clientCh
				clientChannelsMutex.Unlock()

				contadorMutex.Lock()
				contadorClientes++
				clientID := contadorClientes
				contadorMutex.Unlock()

				fmt.Printf("[+] Recibido SYN del nuevo cliente #%d (%s). Iniciando...\n", clientID, clientAddr.String())
				go handleClient(conn, clientAddr, clientID, clientCh)
				
			}
		} else {
			// Es un paquete de un cliente existente, lo enviamos a su goroutine
			select {
			case clientCh <- pkt:
				// Paquete enviado al canal del cliente
			default:
				// El canal está lleno, se omite el paquete para no bloquear
				fmt.Println("[!] Advertencia: canal de cliente lleno para", clientAddr.String())
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

	// --- 1. Handshake ---
	synAckPkt := Packet{Type: "SYN-ACK", Seq: 1}
	sendPacket(conn, addr, synAckPkt)

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

	// --- 2. Implementación de Ventana Deslizante para la Transferencia ---
	archivoAsignado := archivosParaClientes[(clientID-1)%len(archivosParaClientes)+1]
	fmt.Printf("[+] [Cliente #%d] Asignando archivo '%s'.\n", clientID, archivoAsignado)
	file, err := os.Open(archivoAsignado)
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Error al abrir archivo: %s\n", clientID, err)
		return
	}
	defer file.Close()

	var packetsInWindow list.List
	base := 100 // Número de secuencia del primer paquete
	nextSeq := base
	transferComplete := false
	
	ackMu := sync.Mutex{}
	lastAckReceived := 0 // Para manejar los ACKs fuera de orden

	// Goroutine para recibir ACKs y deslizar la ventana
	go func() {
		for {
			select {
			case pkt := <-clientCh:
				if pkt.Type == "ACK" {
					ackMu.Lock()
					// Asegurarse de procesar solo ACKs nuevos o mayores
					if pkt.Seq > lastAckReceived {
						lastAckReceived = pkt.Seq
						fmt.Printf("[*] [Cliente #%d] Recibido ACK acumulativo para Seq#%d\n", clientID, pkt.Seq)
						// Eliminar de la lista todos los paquetes con Seq menor o igual al ACK
						for e := packetsInWindow.Front(); e != nil; {
							p := e.Value.(Packet)
							if p.Seq <= pkt.Seq {
								next := e.Next()
								packetsInWindow.Remove(e)
								e = next
								base++ // Deslizar la ventana
							} else {
								e = e.Next()
							}
						}
					}
					ackMu.Unlock()
				}
				if pkt.Type == "FIN-ACK" {
					fmt.Printf("[*] [Cliente #%d] Recibido FIN-ACK.\n", clientID)
					transferComplete = true
					return
				}
			case <-time.After(RETRY_TIMEOUT):
				// Si la ventana no está vacía, retransmitir el primer paquete
				ackMu.Lock()
				if packetsInWindow.Front() != nil {
					oldestPkt := packetsInWindow.Front().Value.(Packet)
					fmt.Printf("[!] [Cliente #%d] Timeout para Seq#%d. Reenviando...\n", clientID, oldestPkt.Seq)
					sendPacket(conn, addr, oldestPkt)
				}
				ackMu.Unlock()
			}
			if transferComplete {
				return
			}
		}
	}()

	scanner := bufio.NewScanner(file)
	for !transferComplete {
		// Enviar paquetes hasta que la ventana esté llena
		if packetsInWindow.Len() < WINDOW_SIZE && scanner.Scan() {
			line := scanner.Text()
			dataPkt := Packet{Type: "DATA", Seq: nextSeq, Payload: []byte(line)}
			sendPacket(conn, addr, dataPkt)
			packetsInWindow.PushBack(dataPkt)
			fmt.Printf("[+] [Cliente #%d] Enviado paquete Seq#%d\n", clientID, nextSeq)
			nextSeq++
		}
		// Si no hay más líneas que leer, salimos del bucle
		if packetsInWindow.Len() == 0 && !scanner.Scan() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Esperar a que la ventana se vacíe antes de enviar FIN
	for packetsInWindow.Len() > 0 {
		fmt.Printf("[*] [Cliente #%d] Esperando ACKs, ventana con %d paquetes.\n", clientID, packetsInWindow.Len())
		time.Sleep(100 * time.Millisecond)
	}

	// --- 3. Terminación de la conexión ---
	finPkt := Packet{Type: "FIN", Seq: nextSeq}
	sendPacket(conn, addr, finPkt)
	fmt.Printf("[+] [Cliente #%d] Enviado FIN. Esperando FIN-ACK.\n", clientID)
}

func sendPacket(conn *net.UDPConn, addr *net.UDPAddr, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.WriteToUDP(bytes, addr)
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
