
package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "io/fs"
    "net"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

type FileEntry struct {
    Name      string    `json:"name"`
    Extension string    `json:"extension"`
    Published bool      `json:"published"`
    TTL       int       `json:"ttl"`
    LastSeen  time.Time `json:"last_seen"`
}

type Config struct {
    Directory string      `json:"directory"`
    Files     []FileEntry `json:"files"`
}

var (
    configFile = "config.json"
    logFile    = "log.log"
    udpPort    = ":50000"
    mu         sync.Mutex
)

func loadConfig() Config {
    var config Config
    data, err := os.ReadFile(configFile)
    if err != nil {
        return config
    }
    json.Unmarshal(data, &config)
    return config
}

func saveConfig(config Config) {
    data, _ := json.MarshalIndent(config, "", "  ")
    os.WriteFile(configFile, data, 0644)
}

func logChange(message string) {
    f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    defer f.Close()
    timestamp := time.Now().Format(time.RFC3339)
    f.WriteString(fmt.Sprintf("%s - %s\n", timestamp, message))
}

func askUserForFileInfo(name, ext string) (bool, int) {
    reader := bufio.NewReader(os.Stdin)
    fmt.Printf("[!] Deseas publicar el archivo %s%s? (s/n): ", name, ext)
    answer, _ := reader.ReadString('\n')
    answer = strings.TrimSpace(answer)
    if strings.ToLower(answer) == "s" {
        fmt.Print("[!] Ingresa el TTL en minutos: ")
        ttlStr, _ := reader.ReadString('\n')
        ttlStr = strings.TrimSpace(ttlStr)
        var ttl int
        fmt.Sscanf(ttlStr, "%d", &ttl)
        return true, ttl
    }
    return false, 0
}

func scanDirectory(path string, existing []FileEntry) []FileEntry {
    filesMap := make(map[string]FileEntry)
    for _, f := range existing {
        key := f.Name + f.Extension
        filesMap[key] = f
    }

    entries := []FileEntry{}
    filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
        if !d.IsDir() {
            name := d.Name()
            ext := filepath.Ext(name)
            base := strings.TrimSuffix(name, ext)
            key := base + ext
            entry := FileEntry{
                Name:      base,
                Extension: ext,
                Published: false,
                TTL:       0,
                LastSeen:  time.Now(),
            }
            if old, ok := filesMap[key]; ok {
                entry.Published = old.Published
                entry.TTL = old.TTL
            } else {
                logChange("[*] Nuevo archivo detectado: " + key)
                pub, ttl := askUserForFileInfo(base, ext)
                entry.Published = pub
                entry.TTL = ttl
            }
            entries = append(entries, entry)
        }
        return nil
    })

    currentKeys := make(map[string]bool)
    for _, e := range entries {
        currentKeys[e.Name+e.Extension] = true
    }
    for _, old := range existing {
        key := old.Name + old.Extension
        if !currentKeys[key] {
            logChange("[!] Archivo eliminado: " + key)
        }
    }

    return entries
}

func monitorDirectory(config *Config) {
    for {
        mu.Lock()
        config.Files = scanDirectory(config.Directory, config.Files)
        saveConfig(*config)
        mu.Unlock()
        time.Sleep(5 * time.Minute)
    }
}

func startUDPServer(config *Config) {
    addr, _ := net.ResolveUDPAddr("udp", udpPort)
    conn, _ := net.ListenUDP("udp", addr)
    defer conn.Close()
    buf := make([]byte, 1024)

    for {
        n, remote, _ := conn.ReadFromUDP(buf)
        filename := strings.TrimSpace(string(buf[:n]))
        response := "NACK"

        mu.Lock()
        for _, f := range config.Files {
            if f.Name+f.Extension == filename && f.Published {
                response = "ACK"
                break
            }
        }
        mu.Unlock()

        conn.WriteToUDP([]byte(response), remote)
    }
}

func main() {
    config := loadConfig()
    if config.Directory == "" {
        fmt.Print("[!] Ingresa la ruta de la carpeta a monitorear: ")
        fmt.Scanln(&config.Directory)
    }

    config.Files = scanDirectory(config.Directory, config.Files)
    saveConfig(config)

    go monitorDirectory(&config)
    go startUDPServer(&config)

    fmt.Println("[+] Servidor iniciado en el puerto 50000. \n[!] Presiona Ctrl+C para detener.")
    select {}
}

