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

// Mapa para asociar un número de cliente con un nombre de archivo.
var archivosParaClientes = map[int]string{
	1: "archivo1.txt",
	2: "archivo2.txt",
	3: "archivo3.txt",
}

// Mapa para gestionar el estado de cada cliente.
var clientStates = make(map[string]bool)
var clientStatesMutex = &sync.Mutex{}

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
	fmt.Println("[+] Servidor RUDP (dinámico) escuchando en el puerto 8080...")

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
			clientStatesMutex.Lock()
			if _, exists := clientStates[clientAddr.String()]; !exists {
				clientStates[clientAddr.String()] = true
				clientStatesMutex.Unlock()

				contadorMutex.Lock()
				contadorClientes++
				clientID := contadorClientes
				contadorMutex.Unlock()

				fmt.Printf("[+] Recibido SYN del nuevo cliente #%d (%s). Iniciando...\n", clientID, clientAddr.String())
				go handleClient(conn, clientAddr, clientID) // <-- CAMBIO: Pasamos la conexión principal
			} else {
				clientStatesMutex.Unlock()
			}
		}
	}
}

// handleClient ahora acepta el socket de escucha principal
func handleClient(conn *net.UDPConn, addr *net.UDPAddr, clientID int) {
	defer func() {
		clientStatesMutex.Lock()
		delete(clientStates, addr.String())
		clientStatesMutex.Unlock()
		fmt.Printf("[*] Limpiado el estado para el cliente #%d (%s).\n", clientID, addr.String())
	}()

	// --- 1. Realizar el Handshake ---
	synAckPkt := Packet{Type: "SYN-ACK", Seq: 1}
	sendPacketToAddr(conn, addr, synAckPkt) // <-- CAMBIO: Usamos el socket de escucha para enviar

	// Esperamos el ACK del cliente
	_, err := receivePacketWithTimeout(conn, "ACK", 1, addr) // <-- CAMBIO: Esperamos el ACK desde la dirección correcta
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Handshake fallido: %s\n", clientID, err)
		return
	}
	fmt.Printf("[+] [Cliente #%d] Handshake completado.\n", clientID)

	// --- 2. Enviar el archivo dinámicamente ---
	archivoAsignado := archivosParaClientes[(clientID-1)%len(archivosParaClientes)+1]
	fmt.Printf("[*] [Cliente #%d] Asignando archivo '%s'.\n", clientID, archivoAsignado)

	file, err := os.Open(archivoAsignado)
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Error al abrir archivo: %s\n", clientID, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	seqNum := 100
	for scanner.Scan() {
		line := scanner.Text()
		dataPkt := Packet{Type: "DATA", Seq: seqNum, Payload: []byte(line)}

		ackReceived := false
		for i := 0; i < MAX_RETRIES; i++ {
			sendPacketToAddr(conn, addr, dataPkt) // <-- CAMBIO: Usamos el socket de escucha para enviar datos
			ackPkt, err := receivePacketWithTimeout(conn, "ACK", seqNum, addr) // <-- CAMBIO: Esperamos ACK desde la dirección correcta
			if err == nil && ackPkt.Seq == seqNum {
				ackReceived = true
				break
			}
		}

		if !ackReceived {
			fmt.Printf("[!] [Cliente #%d] Falla al enviar paquete Seq#%d. Abortando.\n", clientID, seqNum)
			return
		}
		seqNum++
	}

	// --- 3. Terminar la conexión ---
	finPkt := Packet{Type: "FIN", Seq: seqNum}
	sendPacketToAddr(conn, addr, finPkt) // <-- CAMBIO: Usamos el socket de escucha para enviar FIN
	fmt.Printf("[+] [Cliente #%d] Enviado FIN. Conexión cerrada.\n", clientID)
}

// --- Funciones auxiliares CORREGIDAS ---

// sendPacketToAddr envía un paquete a una dirección específica
func sendPacketToAddr(conn *net.UDPConn, addr *net.UDPAddr, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.WriteToUDP(bytes, addr)
}

// receivePacketWithTimeout recibe un paquete, pero también lo filtra por la dirección esperada
func receivePacketWithTimeout(conn *net.UDPConn, expectedType string, expectedSeq int, expectedAddr *net.UDPAddr) (Packet, error) {
	var pkt Packet
	conn.SetReadDeadline(time.Now().Add(RETRY_TIMEOUT))
	buffer := make([]byte, 1024)
	
	n, receivedAddr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		conn.SetReadDeadline(time.Time{})
		return pkt, err
	}
	
	conn.SetReadDeadline(time.Time{})

	if receivedAddr.String() != expectedAddr.String() {
		return pkt, fmt.Errorf("[!] paquete de una dirección inesperada")
	}

	if err := json.Unmarshal(buffer[:n], &pkt); err != nil {
		return pkt, fmt.Errorf("[!] paquete malformado")
	}

	if pkt.Type != expectedType || pkt.Seq != expectedSeq {
		return pkt, fmt.Errorf("[!] paquete inesperado (Tipo: %s, Seq: %d)", pkt.Type, pkt.Seq)
	}
	return pkt, nil
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
