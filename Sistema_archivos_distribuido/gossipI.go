package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/pion/dtls/v2"
)

// GossipProtocol gestiona la comunicación con otros nodos.
type GossipProtocol struct {
	Peers      []string // Direcciones de los otros servidores
	dtlsConfig *dtls.Config
}

// NewGossipProtocol crea un nuevo protocolo e inicializa la configuración DTLS.
func NewGossipProtocol(peers []string, cert *dtls.Config) (*GossipProtocol, error) {
	return &GossipProtocol{
		Peers:      peers,
		dtlsConfig: cert,
	}, nil
}

// SendUpdate envía un mensaje de tipo GOSSIP_UPDATE a un subconjunto aleatorio de peers.
func (gp *GossipProtocol) SendUpdate(entry DirectoryEntry, action string) {
	// Un enfoque de "gossip" más inteligente: envía a un subconjunto aleatorio de 2 pares.
	peersToSend := gp.GetRandomPeers(2) 
	for _, peerAddr := range peersToSend {
		logEvent("GOSSIP", "SEND_UPDATE", fmt.Sprintf("Enviando %s para '%s' a %s", action, entry.FileName, peerAddr))
		go func(addr string) {
			// Establecer una conexión DTLS
			conn, err := dtls.Dial("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}, gp.dtlsConfig)
			if err != nil {
				logEvent("GOSSIP", "ERROR", fmt.Sprintf("Falla al conectar con peer %s: %v", addr, err))
				return
			}
			defer conn.Close()

			payloadBytes, _ := json.Marshal(entry)
			msg := NetworkMessage{
				Type:    action,
				Payload: payloadBytes,
			}
			msgBytes, _ := json.Marshal(msg)
			conn.Write(msgBytes)
		}(peerAddr)
	}
}

// GetRandomPeers devuelve un subconjunto aleatorio de direcciones de pares.
func (gp *GossipProtocol) GetRandomPeers(n int) []string {
	if len(gp.Peers) <= n {
		return gp.Peers
	}
	
	rand.Seed(time.Now().UnixNano())
	indices := rand.Perm(len(gp.Peers))
	
	selectedPeers := make([]string, n)
	for i := 0; i < n; i++ {
		selectedPeers[i] = gp.Peers[indices[i]]
	}
	return selectedPeers
}

// RequestStatus solicita el estado de un archivo a un peer específico.
func (gp *GossipProtocol) RequestStatus(fileName string, peerAddr string) (*DirectoryEntry, error) {
	// Establecer una conexión DTLS
	conn, err := dtls.Dial("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}, gp.dtlsConfig)
	if err != nil {
		return nil, fmt.Errorf("falla al conectar con peer %s: %v", peerAddr, err)
	}
	defer conn.Close()

	payloadBytes, _ := json.Marshal(fileName)
	msg := NetworkMessage{
		Type:    "REQUEST_STATUS",
		Payload: payloadBytes,
	}
	msgBytes, _ := json.Marshal(msg)
	conn.Write(msgBytes)

	buffer := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // Timeout
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("falla al recibir respuesta de peer %s: %v", peerAddr, err)
	}

	var responseMsg NetworkMessage
	json.Unmarshal(buffer[:n], &responseMsg)

	if responseMsg.Type == "STATUS_RESPONSE" && responseMsg.Authoritative {
		var entry DirectoryEntry
		json.Unmarshal(responseMsg.Payload, &entry)
		return &entry, nil
	}
	return nil, fmt.Errorf("respuesta no autoritativa o NACK recibida")
}
