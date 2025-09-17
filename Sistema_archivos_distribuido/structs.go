package main

import "time"

// DirectoryEntry define la estructura de una entrada en el directorio de archivos.
type DirectoryEntry struct {
    FileName         string    `json:"file_name"`
    Extension        string    `json:"extension"`
    Size             int64     `json:"size"`
    CreationDate     time.Time `json:"creation_date"`
    ModificationDate time.Time `json:"modification_date"`
    Version          int64     `json:"version"` // Nuevo campo
    TTL              int       `json:"ttl"`
    OwnerIP          string    `json:"owner_ip"`
}

// NetworkMessage se mantiene igual
type NetworkMessage struct {
    Type          string `json:"type"`
    Payload       []byte `json:"payload"`
    Authoritative bool   `json:"authoritative"`
    SenderIP      string `json:"sender_ip"`
}

// FileUpdate encapsula los datos necesarios para una actualización de archivo.
type FileUpdate struct {
    FileName         string
    Content          []byte
    Version          int64
    ModificationDate time.Time
}
