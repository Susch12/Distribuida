package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pion/dtls/v2"
)

// Packet defines our RUDP packet structure.
type Packet struct {
	Type    string
	Seq     int
	Payload []byte
}

const (
	RETRY_TIMEOUT = 500 * time.Millisecond
	WINDOW_SIZE   = 10 // Should match the server.
)

func main() {
	serverAddr, err := net.ResolveUDPAddr("udp", "localhost:8080")
	if err != nil {
		panic(err)
	}

	// DTLS configuration.
	dtlsConfig := &dtls.Config{
		InsecureSkipVerify: true, // This is dangerous in production, only for testing.
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}

	// Perform the DTLS handshake.
	fmt.Println("[!] Iniciando handshake DTLS...")
	conn, err := dtls.Dial("udp", serverAddr, dtlsConfig)
	if err != nil {
		fmt.Println("[!] Falla en el handshake DTLS:", err)
		return
	}
	defer conn.Close()
	fmt.Println("[+] Handshake DTLS completado.")

	// --- No need for a 3-way handshake here; DTLS handles it for us. ---

	// Receive the file.
	fileName := fmt.Sprintf("copia_dtls_%s.txt", conn.LocalAddr().String())
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
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buffer)
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

// sendPacket writes to the DTLS connection.
func sendPacket(conn net.Conn, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.Write(bytes)
}
