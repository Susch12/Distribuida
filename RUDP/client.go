package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
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
	WINDOW_SIZE   = 10 // Debe coincidir con el servidor
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

	// --- 1. Handshake ---
	fmt.Println("[!] Iniciando handshake...")
	var synAckPkt Packet
	handshakeComplete := false
	for i := 0; i < MAX_RETRIES; i++ {
		synPkt := Packet{Type: "SYN", Seq: 0}
		sendPacket(conn, synPkt)
		fmt.Printf("[*] Intento %d: Enviado SYN.\n", i+1)

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
	ackPkt := Packet{Type: "ACK", Seq: synAckPkt.Seq}
	sendPacket(conn, ackPkt)
	fmt.Println("[+] Handshake completado.")

	// --- 2. Implementación de Ventana Deslizante para la Recepción ---
	fileName := fmt.Sprintf("copia_rudp_%s.txt", conn.LocalAddr().String())
	file, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	fmt.Println("[*] Recibiendo archivo con ventana deslizante...")
	var mu sync.Mutex
	receiveBuffer := make(map[int]Packet)
	expectedSeq := 100

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
			mu.Lock()
			receiveBuffer[pkt.Seq] = pkt
			mu.Unlock()

			fmt.Printf("[*] Recibido paquete Seq#%d. Verificando secuencia...\n", pkt.Seq)

			// Procesar paquetes en orden desde el búfer
			for {
				if _, ok := receiveBuffer[expectedSeq]; ok {
					p := receiveBuffer[expectedSeq]
					fmt.Printf("[+] Escribiendo en archivo el paquete Seq#%d\n", p.Seq)
					file.Write(p.Payload)
					file.WriteString("\n")
					delete(receiveBuffer, expectedSeq)
					expectedSeq++
				} else {
					break
				}
			}

			// Enviar ACK acumulativo
			ackDataPkt := Packet{Type: "ACK", Seq: expectedSeq - 1}
			sendPacket(conn, ackDataPkt)
			fmt.Printf("[+] Enviando ACK acumulativo para Seq#%d\n", ackDataPkt.Seq)

		} else if pkt.Type == "FIN" {
			fmt.Println("\n[+] Recibido paquete FIN. Terminando.")
			finAckPkt := Packet{Type: "FIN-ACK", Seq: pkt.Seq}
			sendPacket(conn, finAckPkt)
			break
		}
	}

	fmt.Printf("[+] Proceso terminado. Archivo guardado como '%s'\n", fileName)
}

func sendPacket(conn *net.UDPConn, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.Write(bytes)
}
