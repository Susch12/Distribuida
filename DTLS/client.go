package main

import (
	"crypto/x509"
	"encoding/json"
	"flag"
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
	WINDOW_SIZE   = 10 // Debe coincidir con el servidor
)

func main() {
	serverAddrStr := flag.String("server", "localhost:8080", "Dirección del servidor DTLS")
	flag.Parse()

	serverAddr, err := net.ResolveUDPAddr("udp", *serverAddrStr)
	if err != nil {
		panic(err)
	}

	// 1. Mejorando la Seguridad: Cargar el certificado del servidor
	certPEM, err := os.ReadFile("cert.pem")
	if err != nil {
		fmt.Println("[!] No se pudo leer el archivo de certificado. ¿Está el servidor corriendo?")
		return
	}
	
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		panic("Falla al agregar certificado al pool")
	}

	// 2. Configuración DTLS mejorada: ¡Validación de Certificado activada!
	dtlsConfig := &dtls.Config{
		InsecureSkipVerify: false, // Ahora es false para validar el certificado
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
	}

	fmt.Println("[!] Iniciando handshake DTLS y validando certificado del servidor...")
	conn, err := dtls.Dial("udp", serverAddr, dtlsConfig)
	if err != nil {
		fmt.Println("[!] Falla en el handshake DTLS:", err)
		return
	}
	defer conn.Close()
	fmt.Println("[+] Handshake DTLS completado y certificado validado.")

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

			fmt.Printf("[*] Recibido paquete Seq#%d. Enviando ACK...\n", pkt.Seq)

			// Selective Repeat: Enviar un ACK individual para cada paquete recibido
			ackDataPkt := Packet{Type: "ACK", Seq: pkt.Seq}
			sendPacket(conn, ackDataPkt)
			
			// Lógica para reordenar y escribir en el archivo
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
		} else if pkt.Type == "FIN" {
			fmt.Println("\n[+] Recibido paquete FIN. Terminando.")
			finAckPkt := Packet{Type: "FIN-ACK", Seq: pkt.Seq}
			sendPacket(conn, finAckPkt)
			break
		}
	}

	fmt.Printf("[+] Proceso terminado. Archivo guardado como '%s'\n", fileName)
}

func sendPacket(conn net.Conn, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.Write(bytes)
}
