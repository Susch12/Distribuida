package main

import "time"

// DirectoryEntry define la estructura de una entrada en el directorio de archivos.
type DirectoryEntry struct {
	FileName        string
	Extension       string
	Size            int64
	CreationDate    time.Time
	ModificationDate time.Time
	TTL             int
	OwnerIP         string
}

// NetworkMessage define la estructura genérica para la comunicación entre los nodos.
type NetworkMessage struct {
	Type          string      // Tipo de mensaje: "QUERY", "RESPONSE", "UPDATE", "NACK", etc.
	Payload       []byte      // Contenido del mensaje, por ejemplo, JSON de una DirectoryEntry
	Authoritative bool        // true si la respuesta proviene del dueño del archivo
	SenderIP      string      // La dirección IP del nodo que envía el mensaje
}
