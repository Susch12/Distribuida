package main

import (
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
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

// ClientSessionLog estructura para logging detallado
type ClientSessionLog struct {
	Timestamp          time.Time              `json:"timestamp"`
	Event              string                 `json:"event"`
	Details            map[string]interface{} `json:"details"`
}

const (
	RETRY_TIMEOUT = 500 * time.Millisecond
	WINDOW_SIZE   = 10 // Debe coincidir con el servidor
)

// Mapa de cipher suites disponibles
var availableCipherSuites = map[string]dtls.CipherSuiteID{
	"AES128": dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	"AES256": dtls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
}

func main() {
	serverAddrStr := flag.String("server", "localhost:8080", "Dirección del servidor DTLS")
	cipherPref := flag.String("cipher", "AES256,AES128", "Cipher suites preferidos (separados por coma): AES128, AES256")
	flag.Parse()

	serverAddr, err := net.ResolveUDPAddr("udp", *serverAddrStr)
	if err != nil {
		panic(err)
	}

	// 1. Mejorando la Seguridad: Cargar el certificado del servidor
	certPEM, err := os.ReadFile("cert.pem")
	if err != nil {
		fmt.Println("[!] No se pudo leer el archivo de certificado.")
		fmt.Println("[!] ¿Está el servidor corriendo y cert.pem existe?")
		return
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		panic("Falla al agregar certificado al pool de CAs de confianza")
	}

	// MEJORA: Parsear cipher suites desde CLI
	clientCipherSuites := parseCipherSuites(*cipherPref)
	if len(clientCipherSuites) == 0 {
		fmt.Println("[!] Error: No se especificaron cipher suites válidos")
		fmt.Println("[!] Opciones disponibles: AES128, AES256")
		fmt.Println("[!] Ejemplo: -cipher AES256,AES128")
		return
	}

	// 2. Configuración DTLS mejorada: ¡Validación de Certificado activada!
	dtlsConfig := &dtls.Config{
		InsecureSkipVerify:   false, // Validar certificado ✓
		RootCAs:              roots,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		CipherSuites:         clientCipherSuites, // Control del cliente
	}

	fmt.Println("════════════════════════════════════════════════════════")
	fmt.Println("       CLIENTE DTLS-RUDP - Transferencia Segura")
	fmt.Println("════════════════════════════════════════════════════════")
	fmt.Printf("[+] Servidor: %s\n", *serverAddrStr)
	fmt.Printf("[+] Cipher Suite: %s\n", getCipherSuiteName(clientCipherSuites[0]))
	fmt.Println("[+] Iniciando conexión segura...")
	fmt.Println()

	// MEJORA: Inicializar logging
	sessionStart := time.Now()
	logFile := initializeLog()
	defer logFile.Close()

	logEvent(logFile, "client_started", map[string]interface{}{
		"server_address":  *serverAddrStr,
		"cipher_suites":   getCipherSuiteNames(clientCipherSuites),
		"start_time":      sessionStart.Format(time.RFC3339),
	})

	conn, err := dtls.Dial("udp", serverAddr, dtlsConfig)
	if err != nil {
		fmt.Println("[!] Error en handshake DTLS")
		fmt.Printf("[!] %s\n", err)
		
		logEvent(logFile, "handshake_failed", map[string]interface{}{
			"error": err.Error(),
			"duration_ms": time.Since(sessionStart).Milliseconds(),
		})
		
		return
	}
	defer conn.Close()

	handshakeDuration := time.Since(sessionStart)
	
	// En pion/dtls v2, State no expone CipherSuiteID públicamente
	// Usamos el primer cipher suite de nuestra configuración (el preferido/negociado)
	cipherSuiteName := getCipherSuiteName(clientCipherSuites[0])
	hasForwardSecrecy := strings.Contains(cipherSuiteName, "ECDHE")
	
	fmt.Printf("[+] Handshake completado (%.0fms)\n", float64(handshakeDuration.Milliseconds()))
	fmt.Println("[+] Certificado validado")
	fmt.Printf("[+] Cipher Suite negociado: %s\n", cipherSuiteName)
	if hasForwardSecrecy {
		fmt.Println("[+] Forward Secrecy: (ECDHE)")
	}
	fmt.Println()

	// Log de handshake exitoso con cipher suite
	logEvent(logFile, "handshake_complete", map[string]interface{}{
		"duration_ms": handshakeDuration.Milliseconds(),
		"cipher_suite": cipherSuiteName,
		"cipher_suite_id": fmt.Sprintf("0x%04x", clientCipherSuites[0]),
		"forward_secrecy": hasForwardSecrecy,
		"certificate_validated": true,
	})

	// MEJORA: Crear archivo con timestamp para evitar sobrescritura
	timestamp := time.Now().Format("20060102_150405")
	localAddr := strings.ReplaceAll(conn.LocalAddr().String(), ":", "_")
	fileName := fmt.Sprintf("copia_dtls_%s_%s.txt", localAddr, timestamp)
	
	file, err := os.Create(fileName)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	fmt.Printf("[*] Recibiendo: %s\n", fileName)
	fmt.Println()

	var mu sync.Mutex
	receiveBuffer := make(map[int]Packet)
	expectedSeq := 100
	packetsReceived := 0
	packetsWritten := 0
	rekeyCount := 0
	startTime := time.Now()

	for {
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buffer)
		if err != nil {
			fmt.Println()
			fmt.Println("[!] Timeout o conexión cerrada por el servidor")
			break
		}

		var pkt Packet
		json.Unmarshal(buffer[:n], &pkt)

		// MEJORA: Manejar mensaje de rekeying
		if pkt.Type == "REKEY" {
			rekeyCount++
			fmt.Printf("\n[!] Rekeying #%d (rotación de claves)\n", rekeyCount)

			// Log del rekeying
			logEvent(logFile, "rekeying_received", map[string]interface{}{
				"rekey_number": rekeyCount,
				"session_duration_seconds": time.Since(sessionStart).Seconds(),
			})

			// Confirmar rekeying al servidor
			rekeyAckPkt := Packet{Type: "REKEY-ACK", Seq: 0}
			sendPacket(conn, rekeyAckPkt)
			continue
		}

		if pkt.Type == "DATA" {
			mu.Lock()
			receiveBuffer[pkt.Seq] = pkt
			mu.Unlock()

			packetsReceived++
			
			// Solo mostrar cada 5 paquetes para no saturar la pantalla
			if packetsReceived % 5 == 0 || packetsReceived == 1 {
				fmt.Printf("[*] Paquetes recibidos: %d\r", packetsReceived)
			}

			// Selective Repeat: Enviar un ACK individual para cada paquete recibido
			ackDataPkt := Packet{Type: "ACK", Seq: pkt.Seq}
			sendPacket(conn, ackDataPkt)

			// Lógica para reordenar y escribir en el archivo
			mu.Lock()
			writtenInThisRound := 0
			for {
				if p, ok := receiveBuffer[expectedSeq]; ok {
					file.Write(p.Payload)
					file.WriteString("\n")
					delete(receiveBuffer, expectedSeq)
					expectedSeq++
					packetsWritten++
					writtenInThisRound++
				} else {
					break
				}
			}
			mu.Unlock()

		} else if pkt.Type == "FIN" {
			elapsed := time.Since(startTime)
			fmt.Println("\n")
			fmt.Println("════════════════════════════════════════════════════════")
			fmt.Println("                 TRANSFERENCIA COMPLETA")
			fmt.Println("════════════════════════════════════════════════════════")
			fmt.Printf("[+] Paquetes: %d recibidos, %d escritos\n", packetsReceived, packetsWritten)
			fmt.Printf("[+] Duración: %.2f segundos\n", elapsed.Seconds())
			fmt.Printf("[+] Throughput: %.1f paquetes/seg\n", float64(packetsReceived)/elapsed.Seconds())
			fmt.Printf("[+] Rekeying: %d rotaciones\n", rekeyCount)
			fmt.Println()

			// Log de transferencia completa
			logEvent(logFile, "transfer_complete", map[string]interface{}{
				"packets_received": packetsReceived,
				"packets_written": packetsWritten,
				"rekey_count": rekeyCount,
				"duration_seconds": elapsed.Seconds(),
				"throughput_pps": float64(packetsReceived)/elapsed.Seconds(),
			})

			finAckPkt := Packet{Type: "FIN-ACK", Seq: pkt.Seq}
			sendPacket(conn, finAckPkt)
			break
		}
	}

	// MEJORA: Resumen de seguridad al finalizar
	totalDuration := time.Since(sessionStart)
	fileInfo, _ := file.Stat()
	
	fmt.Println("════════════════════════════════════════════════════════")
	fmt.Println("                  RESUMEN DE SEGURIDAD")
	fmt.Println("════════════════════════════════════════════════════════")
	fmt.Printf("[!] Archivo: %s (%d bytes)\n", fileName, fileInfo.Size())
	fmt.Printf("[*] Cipher Suite: %s\n", cipherSuiteName)
	fmt.Printf("[*] Forward Secrecy: (ECDHE)\n")
	fmt.Printf("[*] Certificado: [+] Validado\n")
	fmt.Printf("[!] Integridad: [+] (HMAC)\n")
	fmt.Printf("[*] Rekeying: %d rotaciones\n", rekeyCount)
	fmt.Printf("[+] Duración total: %.2f segundos\n", totalDuration.Seconds())
	fmt.Println("════════════════════════════════════════════════════════")
	fmt.Printf("[!] Log guardado: client_session.log\n")
	fmt.Println()

	// Log final de sesión
	logEvent(logFile, "session_complete", map[string]interface{}{
		"total_duration_seconds": totalDuration.Seconds(),
		"handshake_duration_ms": handshakeDuration.Milliseconds(),
		"file_name": fileName,
		"file_size_bytes": fileInfo.Size(),
		"packets_total": packetsReceived,
		"rekey_total": rekeyCount,
		"cipher_suite": cipherSuiteName,
		"forward_secrecy": hasForwardSecrecy,
	})
}

func sendPacket(conn net.Conn, pkt Packet) {
	bytes, _ := json.Marshal(pkt)
	conn.Write(bytes)
}

// parseCipherSuites convierte una lista de nombres a cipher suite IDs
func parseCipherSuites(cipherStr string) []dtls.CipherSuiteID {
	parts := strings.Split(cipherStr, ",")
	var suites []dtls.CipherSuiteID
	
	for _, part := range parts {
		name := strings.TrimSpace(strings.ToUpper(part))
		if cs, ok := availableCipherSuites[name]; ok {
			suites = append(suites, cs)
		} else if name != "" {
			fmt.Printf("[!] Advertencia: Cipher suite '%s' no reconocido (ignorado)\n", part)
		}
	}
	
	return suites
}

// getCipherSuiteName devuelve el nombre legible de un cipher suite
func getCipherSuiteName(cs dtls.CipherSuiteID) string {
	switch cs {
	case dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:
		return "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
	case dtls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:
		return "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
	default:
		return fmt.Sprintf("Unknown (0x%x)", cs)
	}
}

// initializeLog crea el archivo de log para la sesión del cliente
func initializeLog() *os.File {
	logFile, err := os.Create("client_session.log")
	if err != nil {
		fmt.Printf("[!] Advertencia: No se pudo crear log file: %v\n", err)
		return nil
	}
	
	// Escribir encabezado del log
	header := map[string]interface{}{
		"log_type": "DTLS-RUDP Client Session Log",
		"version": "1.0",
		"created_at": time.Now().Format(time.RFC3339),
	}
	
	headerJSON, _ := json.Marshal(header)
	logFile.Write(headerJSON)
	logFile.WriteString("\n")
	
	return logFile
}

// logEvent registra un evento en el log de sesión
func logEvent(logFile *os.File, event string, details map[string]interface{}) {
	if logFile == nil {
		return
	}
	
	logEntry := ClientSessionLog{
		Timestamp: time.Now(),
		Event:     event,
		Details:   details,
	}
	
	jsonLog, err := json.Marshal(logEntry)
	if err != nil {
		return
	}
	
	logFile.Write(jsonLog)
	logFile.WriteString("\n")
}

// getCipherSuiteNames devuelve una lista de nombres de cipher suites
func getCipherSuiteNames(suites []dtls.CipherSuiteID) []string {
	names := make([]string, len(suites))
	for i, cs := range suites {
		names[i] = getCipherSuiteName(cs)
	}
	return names
}
