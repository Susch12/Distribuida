package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileInfo struct {
    Name      string    `json:"name"`
    Extension string    `json:"extension"`
    Publish   bool      `json:"publish"`
    TTL       int       `json:"ttl"`
    LastSeen  time.Time `json:"-"` // No serializar directamente
}

type Config struct {
	WatchFolder string     `json:"watchFolder"`
	TTL         int        `json:"ttl"`
	Files       []FileInfo `json:"files"`
}


// Para serializar/deserializar LastSeen manualmente
type fileInfoJSON struct {
    Name      string `json:"name"`
    Extension string `json:"extension"`
    Publish   bool   `json:"publish"`
    TTL       int    `json:"ttl"`
    LastSeen  string `json:"lastSeen,omitempty"`
}

var (
	configFile = "config.json"
	config     Config
	mutex      = &sync.Mutex{}
	inputChan  = make(chan string)
)

func (f *FileInfo) MarshalJSON() ([]byte, error) {
    aux := fileInfoJSON{
        Name:      f.Name,
        Extension: f.Extension,
        Publish:   f.Publish,
        TTL:       f.TTL,
        LastSeen:  f.LastSeen.Format(time.RFC3339),
    }
    return json.Marshal(aux)
}

func (f *FileInfo) UnmarshalJSON(data []byte) error {
    var aux fileInfoJSON
    if err := json.Unmarshal(data, &aux); err != nil {
        return err
    }
    
    f.Name = aux.Name
    f.Extension = aux.Extension
    f.Publish = aux.Publish
    f.TTL = aux.TTL
    
    if aux.LastSeen != "" {
        if t, err := time.Parse(time.RFC3339, aux.LastSeen); err == nil {
            f.LastSeen = t
        } else {
            f.LastSeen = time.Now()
        }
    } else {
        f.LastSeen = time.Now()
    }
    
    return nil
}

func loadConfig() error {
	mutex.Lock()
	defer mutex.Unlock()

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("[!] No se encontró el archivo de configuración. Creando uno nuevo.")
		config = Config{
			WatchFolder: "./",
			TTL:         300,
			Files:       []FileInfo{},
		}
		return saveConfig()
	}

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &config)
}

func saveConfig() error {
	mutex.Lock()
	defer mutex.Unlock()
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(configFile, data, 0644)
}

func readInput() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			inputChan <- input
		}
	}
}

func processUserInput() {
	for {
		select {
		case input := <-inputChan:
			parts := strings.Fields(input)
			if len(parts) == 0 {
				continue
			}

			command := parts[0]
			
			// Comando help sin parámetros
			if command == "help" {
				printHelp()
				continue
			}
			
			if len(parts) < 2 {
				fmt.Println("[!] Formato incorrecto. Uso: [comando] [nombre_archivo]")
				fmt.Println("    Use 'help' para ver todos los comandos disponibles")
				continue
			}

			filename := parts[1]

			mutex.Lock()
			found := false
			for i := range config.Files {
				if config.Files[i].Name == filename {
					found = true
					switch command {
					case "publish":
						config.Files[i].Publish = true
						fmt.Printf("[+] Archivo '%s' marcado para publicación\n", filename)
						log.Printf("[+] Archivo '%s' marcado para publicación", filename)
					case "unpublish":
						config.Files[i].Publish = false
						fmt.Printf("[+] Archivo '%s' desmarcado para publicación\n", filename)
						log.Printf("[+] Archivo '%s' desmarcado para publicación", filename)
					case "setttl":
						if len(parts) < 3 {
							fmt.Println("[!] Falta el valor de TTL. Uso: setttl [nombre_archivo] [segundos]")
							mutex.Unlock()
							continue
						}
						var ttl int
						if _, err := fmt.Sscanf(parts[2], "%d", &ttl); err != nil {
							fmt.Printf("[!] TTL inválido: %v\n", err)
							mutex.Unlock()
							continue
						}
						config.Files[i].TTL = ttl
						fmt.Printf("[+] TTL de '%s' establecido a %d segundos\n", filename, ttl)
						log.Printf("[+] TTL de '%s' establecido a %d segundos", filename, ttl)
					default:
						fmt.Printf("[!] Comando desconocido: %s. Use 'help' para ver comandos disponibles\n", command)
					}
					break
				}
			}

			if !found {
				fmt.Printf("[!] Archivo '%s' no encontrado en la lista. Espere al próximo escaneo si es nuevo.\n", filename)
			}

			if err := saveConfig(); err != nil {
				log.Printf("[!] Error guardando configuración: %v", err)
			}
			mutex.Unlock()
		}
	}
}

func fileMonitor() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	updateFileList()

	for range ticker.C {
		updateFileList()
	}
}

func updateFileList() {
	mutex.Lock()
	defer mutex.Unlock()

	filesToIgnore := map[string]bool{
		"config.json":      true,
		"file_monitor.log": true,
	}

	log.Println("[+] Escaneando carpeta...")

	existing := make(map[string]int)
	for i, f := range config.Files {
		existing[f.Name] = i
	}

	entries, err := ioutil.ReadDir(config.WatchFolder)
	if err != nil {
		log.Printf("[!] Error leyendo carpeta %s: %v", config.WatchFolder, err)
		return
	}

	present := make(map[string]bool)
	now := time.Now()

	for _, e := range entries {
		if e.IsDir() || filesToIgnore[e.Name()] {
			continue
		}

		name := e.Name()
		present[name] = true

		if idx, ok := existing[name]; ok {
			config.Files[idx].LastSeen = now
		} else {
			ext := filepath.Ext(name)
			newFile := FileInfo{
				Name:      name,
				Extension: ext,
				Publish:   false,
				TTL:       config.TTL,
				LastSeen:  now,
			}
			config.Files = append(config.Files, newFile)
			log.Printf("[+] Nuevo archivo detectado: %s (publish=false, ttl=%ds)", name, config.TTL)
			// Mensaje mejorado para el usuario
			fmt.Printf("[+] Nuevo archivo detectado: %s. Use 'publish %s' para habilitar su distribución\n", name, name)
		}
	}

	var kept []FileInfo
	for _, f := range config.Files {
		if present[f.Name] {
			kept = append(kept, f)
		} else {
			age := time.Since(f.LastSeen)
			if age < time.Duration(f.TTL)*time.Second {
				kept = append(kept, f)
			} else {
				log.Printf("[!] Archivo removido o expirado: %s (age=%s, ttl=%ds)", f.Name, age, f.TTL)
			}
		}
	}
	config.Files = kept

	if err := saveConfig(); err != nil {
		log.Printf("[!] Error guardando configuración: %v", err)
	}
	log.Println("[+] Escaneo completo.")
}

func udpServer() {
	addr, err := net.ResolveUDPAddr("udp", ":50000")
	if err != nil {
		log.Fatalf("[!] Error al resolver la dirección UDP: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("[!] Error al iniciar el servidor UDP: %v", err)
	}
	defer conn.Close()

	log.Println("[+] Servidor UDP escuchando en el puerto 50000")

	buf := make([]byte, 1024)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("[!] Error al leer del socket UDP: %v", err)
			continue
		}

		requested := string(buf[:n])
		log.Printf("[+] Solicitud de %s para '%s'", remote, requested)

		response := "NACK"
		now := time.Now()

		mutex.Lock()
		for _, f := range config.Files {
			if f.Name == requested && f.Publish {
				if now.Sub(f.LastSeen) < time.Duration(f.TTL)*time.Second {
					response = "ACK"
				} else {
					log.Printf("[!] TTL expirado para '%s'", f.Name)
				}
				break
			}
		}
		mutex.Unlock()

		if _, err = conn.WriteToUDP([]byte(response), remote); err != nil {
			log.Printf("[!] Error al enviar respuesta UDP a %s: %v", remote, err)
		} else {
			log.Printf("[+] Respuesta '%s' enviada a %s", response, remote)
		}
	}
}

func printHelp() {
	fmt.Println(`
Comandos disponibles:
  publish [nombre_archivo]    - Marcar archivo para publicación
  unpublish [nombre_archivo]  - Desmarcar archivo para publicación
  setttl [nombre_archivo] [segundos] - Establecer TTL para un archivo
  help                        - Mostrar esta ayuda
`)
}

func main() {
	logFile, err := os.OpenFile("file_monitor.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("[!] Error al abrir el archivo de log: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.Println("--- INICIO DEL PROCESO ---")

	if err := loadConfig(); err != nil {
		log.Fatalf("[!] Error al cargar la configuración: %v", err)
	}

	if config.WatchFolder == "" || config.WatchFolder == "./" {
		fmt.Print("[+] Ingrese la ruta de la carpeta a monitorear: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			config.WatchFolder = strings.TrimSpace(scanner.Text())
		}
		if err := saveConfig(); err != nil {
			log.Fatalf("[!] Error al guardar configuración: %v", err)
		}
	}

	fmt.Printf("[+] Monitoreando carpeta: %s\n", config.WatchFolder)
	fmt.Println("[+] Comandos disponibles: publish, unpublish, setttl, help")

	go readInput()
	go processUserInput()
	go fileMonitor()
	go udpServer()

	printHelp()

	// Mantener el programa en ejecución
	select {}
}
