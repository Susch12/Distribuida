package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Packet define nuestra estructura de paquete RUDP
type Packet struct {
	Type    string
	Seq     int
	Payload []byte
}

const (
	RETRY_TIMEOUT = 500 * time.Millisecond
	MAX_RETRIES   = 5
)

func main() {
	serverAddr, err := net.ResolveUDPAddr("udp", "localhost:8080")
	if err != nil {
		panic(err)
	}
	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// --- 1. Realizar el Handshake ---
	fmt.Println("[!] Iniciando handshake...")

	var synAckPkt Packet
	handshakeComplete := false

	for i := 0; i < MAX_RETRIES; i++ {
		// Enviar SYN
		synPkt := Packet{Type: "SYN", Seq: 0}
		sendPacket(conn, synPkt)
		fmt.Printf("[*] Intento %d: Enviado SYN.\n", i+1)

		// Esperar por SYN-ACK con timeout
		conn.SetReadDeadline(time.Now().Add(RETRY_TIMEOUT))
		buffer := make([]byte, 1024)
		n, _, err := conn.ReadFromUDP(buffer)
		conn.SetReadDeadline(time.Time{})

		if err == nil {
			json.Unmarshal(buffer[:n], &synAckPkt)
			if synAckPkt.Type == "SYN-ACK" && synAckPkt.Seq == 1 {
				handshakeComplete = true
				break
			}
		}
	}

	if !handshakeComplete {
		fmt.Println("[!] Falla en el handshake (no se recibió SYN-ACK después de varios reintentos).")
		return
	}
	fmt.Println("[*] Recibido SYN-ACK.")

	// Enviar ACK final del handshake
	ackPkt := Packet{Type: "ACK", Seq: synAckPkt.Seq}
	sendPacket(conn, ackPkt)
	fmt.Println("[+] Handshake completado.")

	// --- 2. Recibir el archivo ---
	fileName := fmt.Sprintf("copia_rudp_%s.txt", conn.LocalAddr().String())
	file, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	fmt.Println("[*] Recibiendo archivo...")
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buffer := make([]byte, 1024)
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("\n[!] Error o conexión cerrada por el servidor:", err)
			break
		}

		var pkt Packet
		json.Unmarshal(buffer[:n], &pkt)

		if pkt.Type == "DATA" {
			fmt.Printf("[+] Recibido Seq#%d, enviando ACK...\n", pkt.Seq)
			file.Write(pkt.Payload)
			file.WriteString("\n")
			ackDataPkt := Packet{Type: "ACK", Seq: pkt.Seq}
			sendPacket(conn, ackDataPkt)
		} else if pkt.Type == "FIN" {
			fmt.Println("\n[+] Recibido paquete FIN. Terminando.")
			break
		}
	}

	fmt.Printf("[+] Proceso terminado. Archivo guardado como '%s'\n", fileName)
}

// --- Funciones auxiliares ---

func sendPacket(conn *net.UDPConn, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.Write(bytes)
}

func receivePacket(conn *net.UDPConn, expectedType string, expectedSeq int) (Packet, error) {
	var pkt Packet
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return pkt, err
	}
	conn.SetReadDeadline(time.Time{})

	json.Unmarshal(buffer[:n], &pkt)
	if pkt.Type != expectedType || pkt.Seq != expectedSeq {
		return pkt, fmt.Errorf("[!] paquete inesperado")
	}
	return pkt, nil
}
