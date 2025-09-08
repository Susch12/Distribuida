package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync" // <-- CAMBIO: Importamos sync para manejar el contador de forma segura
	"time"
)

// Packet define nuestra estructura de paquete RUDP
type Packet struct {
	Type    string
	Seq     int
	Payload []byte
}

// Mapa para asociar un número de cliente con un nombre de archivo.
var archivosParaClientes = map[int]string{ // <-- CAMBIO
	1: "archivo1.txt",
	2: "archivo2.txt",
	3: "archivo3.txt",
}

// Mapa para gestionar el estado de cada cliente.
var clientStates = make(map[string]bool)
var clientStatesMutex = &sync.Mutex{} // Mutex para proteger el acceso a clientStates

var contadorClientes = 0          // <-- CAMBIO: Contador global de clientes
var contadorMutex = &sync.Mutex{} // Mutex para proteger el contador

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

		// Si es un paquete SYN de un cliente nuevo, se inicia la goroutine
		if pkt.Type == "SYN" {
			clientStatesMutex.Lock()
			if _, exists := clientStates[clientAddr.String()]; !exists {
				clientStates[clientAddr.String()] = true // Marcar como "conectando"
				clientStatesMutex.Unlock()

				contadorMutex.Lock()
				contadorClientes++
				clientID := contadorClientes // Asignar el ID actual
				contadorMutex.Unlock()

				fmt.Printf("[+] Recibido SYN del nuevo cliente #%d (%s). Iniciando...\n", clientID, clientAddr.String())
				// <-- CAMBIO: Pasamos el ID del cliente a la goroutine
				go handleClient(clientAddr, clientID)
			} else {
				// Si ya existe, es un SYN duplicado, lo ignoramos
				clientStatesMutex.Unlock()
			}
		}
	}
}

// handleClient ahora acepta un clientID para saber qué archivo enviar
func handleClient(addr *net.UDPAddr, clientID int) { // <-- CAMBIO
	// Limpieza al finalizar la función
	defer func() {
		clientStatesMutex.Lock()
		delete(clientStates, addr.String())
		clientStatesMutex.Unlock()
		fmt.Printf("[*] Limpiado el estado para el cliente #%d (%s).\n", clientID, addr.String())
	}()

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Error al conectar: %s\n", clientID, err)
		return
	}
	defer conn.Close()

	// --- 1. Realizar el Handshake ---
	synAckPkt := Packet{Type: "SYN-ACK", Seq: 1}
	sendPacket(conn, synAckPkt)

	_, err = receivePacketWithTimeout(conn, "ACK", 1)
	if err != nil {
		fmt.Printf("[!] [Cliente #%d] Handshake fallido: %s\n", clientID, err)
		return
	}
	fmt.Printf("[+] [Cliente #%d] Handshake completado.\n", clientID)

	// --- 2. Enviar el archivo dinámicamente ---
	// <-- CAMBIO: Seleccionar el archivo usando el ID del cliente y el operador módulo
	// Esto hace que los archivos se ciclen (1, 2, 3, 1, 2, 3...)
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
	for scanner.Scan() {
		line := scanner.Text()
		dataPkt := Packet{Type: "DATA", Seq: seqNum, Payload: []byte(line)}

		ackReceived := false
		for i := 0; i < MAX_RETRIES; i++ {
			sendPacket(conn, dataPkt)
			ackPkt, err := receivePacketWithTimeout(conn, "ACK", seqNum)
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
	sendPacket(conn, finPkt)
	fmt.Printf("[+] [Cliente #%d] Enviado FIN. Conexión cerrada.\n", clientID)
}

// --- Funciones auxiliares (sin cambios) ---

func sendPacket(conn *net.UDPConn, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.Write(bytes)
}

func receivePacketWithTimeout(conn *net.UDPConn, expectedType string, expectedSeq int) (Packet, error) {
	var pkt Packet
	conn.SetReadDeadline(time.Now().Add(RETRY_TIMEOUT))
	buffer := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return pkt, err
	}
	conn.SetReadDeadline(time.Time{})

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
			fmt.Sprintf("[*] Este es el contenido único del archivo %d.", i),
			"Línea de prueba A.",
			"Línea de prueba B.",
			"Fin del archivo.",
		}
		os.WriteFile(nombreArchivo, []byte(strings.Join(contenido, "\n")), 0644)
	}
	fmt.Println("[*] Archivos de ejemplo creados: archivo1.txt, archivo2.txt, archivo3.txt")
}
