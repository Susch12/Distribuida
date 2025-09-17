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

// PeerState guarda la información de cada peer conocido.
type PeerState struct {
	LastSeen time.Time
}

// GossipProtocol gestiona la comunicación con otros nodos.
type GossipProtocol struct {
	Peers           map[string]PeerState
	mu              sync.RWMutex
	dtlsConfig      *dtls.Config
	selfAddr        string
	knownListenAddrs []string 
	heartbeatTimeout time.Duration
}

// NewGossipProtocol crea un nuevo protocolo e inicializa la configuración DTLS.
func NewGossipProtocol(peers []string, dtlsConfig *dtls.Config, selfAddr string) (*GossipProtocol, error) {
	gp := &GossipProtocol{
		Peers:            make(map[string]PeerState),
		dtlsConfig:       dtlsConfig,
		selfAddr:         selfAddr,
		knownListenAddrs: peers,
		heartbeatTimeout: 60 * time.Second,
	}
	for _, peer := range peers {
		if peer != selfAddr {
			gp.Peers[peer] = PeerState{LastSeen: time.Now()}
		}
	}
	return gp, nil
}

// AddPeer ahora filtra por direcciones de escucha conocidas para evitar puertos efímeros.
func (gp *GossipProtocol) AddPeer(peerAddr string) {
	isKnownPeer := false
	for _, known := range gp.knownListenAddrs {
		if peerAddr == known {
			isKnownPeer = true
			break
		}
	}
	if !isKnownPeer {
		return
	}

	gp.mu.Lock()
	defer gp.mu.Unlock()
	if peerAddr != gp.selfAddr {
		if _, exists := gp.Peers[peerAddr]; !exists {
			gp.Peers[peerAddr] = PeerState{LastSeen: time.Now()}
			logEvent("GOSSIP", "PEER_DISCOVERY", fmt.Sprintf("Nuevo peer descubierto: %s", peerAddr))
		} else {
			peerState := gp.Peers[peerAddr]
			peerState.LastSeen = time.Now()
			gp.Peers[peerAddr] = peerState
		}
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

// SendUpdate envía un mensaje de actualización a un subconjunto aleatorio de peers.
func (gp *GossipProtocol) SendUpdate(entry DirectoryEntry, action string) {
	peersToSend := gp.GetRandomPeers(2)
	for _, peerAddr := range peersToSend {
		logEvent("GOSSIP", "SEND_UPDATE", fmt.Sprintf("Enviando %s para '%s' a %s", action, entry.FileName, peerAddr))
		go func(addr string) {
			conn, err := gp.connectToPeer(addr)
			if err != nil {
				logEvent("GOSSIP", "ERROR", fmt.Sprintf("Falla al conectar para chismorreo con %s: %v", addr, err))
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

// GossipUpdateAllPeers envía una actualización a todos los peers conocidos.
func (gp *GossipProtocol) GossipUpdateAllPeers(entry DirectoryEntry) {
	gp.mu.RLock()
	peers := make([]string, 0, len(gp.Peers))
	for peer := range gp.Peers {
		peers = append(peers, peer)
	}
	gp.mu.RUnlock()

	payloadBytes, _ := json.Marshal(entry)
	msg := NetworkMessage{
		Type:    "GOSSIP_UPDATE",
		Payload: payloadBytes,
	}
	msgBytes, _ := json.Marshal(msg)

	for _, peerAddr := range peers {
		go func(addr string) {
			conn, err := gp.connectToPeer(addr)
			if err != nil {
				logEvent("GOSSIP", "ERROR", fmt.Sprintf("Falla al conectar para chismorreo con %s: %v", addr, err))
				return
			}
			defer conn.Close()
			conn.Write(msgBytes)
			logEvent("GOSSIP", "SEND_UPDATE", fmt.Sprintf("Enviando GOSSIP_UPDATE para '%s' a %s", entry.FileName, addr))
		}(peerAddr)
	}
}

// connectToPeer es una función auxiliar para establecer una conexión DTLS
func (gp *GossipProtocol) connectToPeer(peerAddr string) (net.Conn, error) {
	peerUDPAddr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		return nil, fmt.Errorf("falla al resolver dirección de peer %s: %v", peerAddr, err)
	}
	conn, err := dtls.Dial("udp", peerUDPAddr, gp.dtlsConfig)
	if err != nil {
		return nil, fmt.Errorf("falla al conectar con peer %s: %v", peerAddr, err)
	}
	return conn, nil
}


// RequestStatus solicita el estado de un archivo a un peer específico.
func (gp *GossipProtocol) RequestStatus(fileName string, peerAddr string) (*DirectoryEntry, error) {
	conn, err := gp.connectToPeer(peerAddr)
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
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
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

// SendHeartbeat envía un mensaje de HEARTBEAT a un subconjunto aleatorio de peers.
func (gp *GossipProtocol) SendHeartbeat() {
	peersToSend := gp.GetRandomPeers(3) 
	for _, peerAddr := range peersToSend {
		go func(addr string) {
			logEvent("HEARTBEAT", "SEND", fmt.Sprintf("Enviando heartbeat a %s", addr))
			conn, err := gp.connectToPeer(addr)
			if err != nil {
				logEvent("HEARTBEAT", "ERROR", fmt.Sprintf("Falla al enviar heartbeat a peer %s: %v", addr, err))
				return
			}
			defer conn.Close()

			msg := NetworkMessage{
				Type:    "HEARTBEAT",
				Payload: []byte{},
			}
			msgBytes, _ := json.Marshal(msg)
			conn.Write(msgBytes)
		}(peerAddr)
	}
}

// CheckDeadPeers elimina los peers que no han enviado un heartbeat en un tiempo.
func (gp *GossipProtocol) CheckDeadPeers() {
	gp.mu.Lock()
	defer gp.mu.Unlock()
	for peerAddr, state := range gp.Peers {
		if time.Since(state.LastSeen) > gp.heartbeatTimeout {
			logEvent("HEARTBEAT", "PEER_DEAD", fmt.Sprintf("Peer %s considerado muerto. Eliminando de la lista.", peerAddr))
			delete(gp.Peers, peerAddr)
		}
	}
}

// StartGossipRoutine ahora también inicia la rutina de heartbeats
func (gp *GossipProtocol) StartGossipRoutine() {
	gossipTicker := time.NewTicker(20 * time.Second)
	heartbeatTicker := time.NewTicker(10 * time.Second)
	cleanupTicker := time.NewTicker(30 * time.Second)

	defer gossipTicker.Stop()
	defer heartbeatTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-gossipTicker.C:
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

			rand.Seed(time.Now().UnixNano())
			targetPeer := peerList[rand.Intn(len(peerList))]

			requestMsg := NetworkMessage{
				Type:    "GET_FULL_LIST",
				Payload: []byte{},
			}
			requestBytes, _ := json.Marshal(requestMsg)

			conn, err := gp.connectToPeer(targetPeer)
			if err != nil {
				logEvent("GOSSIP_ROUTINE", "ERROR", fmt.Sprintf("Falla al conectar para chismorreo con %s: %v", targetPeer, err))
				continue
			}
			
			conn.Write(requestBytes)
			
			buffer := make([]byte, 4096)
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := conn.Read(buffer)
			conn.Close()

			if err != nil {
				logEvent("GOSSIP_ROUTINE", "ERROR", fmt.Sprintf("Falla al leer respuesta de chismorreo de %s: %v", targetPeer, err))
				continue
			}

			var responseMsg NetworkMessage
			json.Unmarshal(buffer[:n], &responseMsg)

			if responseMsg.Type == "RESPONSE_LIST" {
				var receivedFiles map[string]DirectoryEntry
				json.Unmarshal(responseMsg.Payload, &receivedFiles)

				sharedFilesMutex.Lock()
				for fileName, entry := range receivedFiles {
					if existingEntry, found := sharedFiles[fileName]; !found || entry.Version > existingEntry.Version {
						sharedFiles[fileName] = entry
						logEvent("GOSSIP_ROUTINE", "MERGE_UPDATE", fmt.Sprintf("Actualización de chismorreo para '%s' con versión %d desde %s", fileName, entry.Version, targetPeer))
					}
				}
				sharedFilesMutex.Unlock()

				gp.AddPeer(targetPeer)
			}

		case <-heartbeatTicker.C:
			gp.SendHeartbeat()

		case <-cleanupTicker.C:
			gp.CheckDeadPeers()
		}
	}
}

func StartGossip(gp *GossipProtocol) {
	go gp.StartGossipRoutine()
}
