package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/pion/dtls/v2"
)

// GossipProtocol gestiona la comunicación con otros nodos.
type GossipProtocol struct {
	Peers      map[string]bool // Direcciones de los otros servidores
	mu         sync.RWMutex    // Mutex para concurrencia segura
	dtlsConfig *dtls.Config
	selfAddr   string // La dirección de este servidor
}

// NewGossipProtocol crea un nuevo protocolo e inicializa la configuración DTLS.
func NewGossipProtocol(peers []string, dtlsConfig *dtls.Config, selfAddr string) (*GossipProtocol, error) {
	gp := &GossipProtocol{
		Peers:      make(map[string]bool),
		dtlsConfig: dtlsConfig,
		selfAddr:   selfAddr,
	}
	for _, peer := range peers {
		if peer != selfAddr {
			gp.Peers[peer] = true
		}
	}
	return gp, nil
}

// AddPeer añade un nuevo peer a la lista de conocidos de forma concurrente.
func (gp *GossipProtocol) AddPeer(peerAddr string) {
	gp.mu.Lock()
	defer gp.mu.Unlock()
	if _, exists := gp.Peers[peerAddr]; !exists {
		gp.Peers[peerAddr] = true
		logEvent("GOSSIP", "PEER_DISCOVERY", fmt.Sprintf("Nuevo peer descubierto: %s", peerAddr))
	}
}

// GetRandomPeers devuelve un subconjunto aleatorio de direcciones de pares.
func (gp *GossipProtocol) GetRandomPeers(n int) []string {
	gp.mu.RLock()
	defer gp.mu.RUnlock()
	
	peerList := make([]string, 0, len(gp.Peers))
	for peer := range gp.Peers {
		peerList = append(peerList, peer)
	}

	if len(peerList) <= n {
		return peerList
	}
	
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(peerList), func(i, j int) {
		peerList[i], peerList[j] = peerList[j], peerList[i]
	})
	
	return peerList[:n]
}

// SendUpdate envía un mensaje de tipo GOSSIP_UPDATE a un subconjunto aleatorio de peers.
func (gp *GossipProtocol) SendUpdate(entry DirectoryEntry, action string) {
	peersToSend := gp.GetRandomPeers(2)
	for _, peerAddr := range peersToSend {
		logEvent("GOSSIP", "SEND_UPDATE", fmt.Sprintf("Enviando %s para '%s' a %s", action, entry.FileName, peerAddr))
		go func(addr string) {
			// Resuelve la dirección del peer
			peerUDPAddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				logEvent("GOSSIP", "ERROR", fmt.Sprintf("Falla al resolver dirección de peer %s: %v", addr, err))
				return
			}
			
			// Establecer una conexión DTLS
			conn, err := dtls.Dial("udp", peerUDPAddr, gp.dtlsConfig)
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

// RequestStatus solicita el estado de un archivo a un peer específico.
func (gp *GossipProtocol) RequestStatus(fileName string, peerAddr string) (*DirectoryEntry, error) {
	// Resuelve la dirección del peer
	peerUDPAddr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		return nil, fmt.Errorf("falla al resolver dirección de peer %s: %v", peerAddr, err)
	}
	
	// Establecer una conexión DTLS
	conn, err := dtls.Dial("udp", peerUDPAddr, gp.dtlsConfig)
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

// Inicia un proceso de "gossip" periódico
func (gp *GossipProtocol) StartGossipRoutine() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		logEvent("GOSSIP_ROUTINE", "INIT", "Iniciando rutina de chismes.")
		
		gp.mu.RLock()
		peerList := make([]string, 0, len(gp.Peers))
		for peer := range gp.Peers {
			peerList = append(peerList, peer)
		}
		gp.mu.RUnlock()

		if len(peerList) == 0 {
			logEvent("GOSSIP_ROUTINE", "WARNING", "No hay peers conocidos para chismorrear.")
			continue
		}

		// Selecciona un peer al azar para el chismorreo
		rand.Seed(time.Now().UnixNano())
		targetPeer := peerList[rand.Intn(len(peerList))]

		// Pide el estado completo al peer, no solo los archivos
		requestMsg := NetworkMessage{
			Type:    "GET_FULL_LIST",
			Payload: []byte{},
		}
		requestBytes, _ := json.Marshal(requestMsg)
		
		peerUDPAddr, err := net.ResolveUDPAddr("udp", targetPeer)
		if err != nil {
			logEvent("GOSSIP_ROUTINE", "ERROR", fmt.Sprintf("Falla al resolver dirección de peer %s: %v", targetPeer, err))
			continue
		}

		conn, err := dtls.Dial("udp", peerUDPAddr, gp.dtlsConfig)
		if err != nil {
			logEvent("GOSSIP_ROUTINE", "ERROR", fmt.Sprintf("Falla al conectar para chismorreo con %s: %v", targetPeer, err))
			continue
		}
		defer conn.Close()
		
		conn.Write(requestBytes)
		
		buffer := make([]byte, 4096)
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := conn.Read(buffer)
		if err != nil {
			logEvent("GOSSIP_ROUTINE", "ERROR", fmt.Sprintf("Falla al leer respuesta de chismorreo de %s: %v", targetPeer, err))
			continue
		}

		var responseMsg NetworkMessage
		json.Unmarshal(buffer[:n], &responseMsg)
		
		if responseMsg.Type == "RESPONSE_LIST" {
			var receivedFiles map[string]DirectoryEntry
			json.Unmarshal(responseMsg.Payload, &receivedFiles)
			
			// Fusiona las listas
			for fileName, entry := range receivedFiles {
				sharedFilesMutex.Lock()
				if existingEntry, found := sharedFiles[fileName]; !found || entry.ModificationDate.After(existingEntry.ModificationDate) {
					sharedFiles[fileName] = entry
					logEvent("GOSSIP_ROUTINE", "MERGE_UPDATE", fmt.Sprintf("Actualización de chismorreo para '%s' desde %s", fileName, targetPeer))
				}
				sharedFilesMutex.Unlock()
			}
			// Añade al peer a la lista de conocidos (si no está ya)
			gp.AddPeer(targetPeer)
		}
	}
}

// Inicia el proceso de chismorreo en un nuevo hilo.
func StartGossip(gp *GossipProtocol) {
	go gp.StartGossipRoutine()
}
