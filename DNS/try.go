package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileInfo struct {
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Publish   bool   `json:"publish"`
  TTL       int       `json:"ttl"`
  LastSeen  time.Time `json:"lastSeen"`
}

type Config struct {
	WatchFolder string     `json:"watchFolder"`
	TTL         int        `json:"ttl"`
	Files       []FileInfo `json:"files"`
}

type Packet struct {
  Data string `json:"data"`
  TTL  int    `json:"ttl"`
}


var (
	configFile = "config.json"
	config     Config
	mutex      = &sync.Mutex{} // Mutex para proteger el acceso concurrente a la configuración.
)


// loadConfig carga la configuración desde el archivo JSON.
// Si el archivo no existe, crea uno con valores predeterminados.
func loadConfig() error {
	mutex.Lock()
	defer mutex.Unlock()

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("[!] No se encontró el archivo de configuración. Creando uno nuevo.")
		// Valores iniciales si no existe el archivo
		config = Config{
			WatchFolder: "./", // Carpeta actual por defecto
			TTL:         300,  // 5 minutos por defecto
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

// saveConfig guarda la configuración actual en el archivo JSON.
func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(configFile, data, 0644)
}


// fileMonitor es la función que se ejecuta en un hilo para monitorear la carpeta.
func fileMonitor() {
	// Usamos un Ticker para ejecutar la tarea cada 5 minutos.
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	// Ejecutamos una vez inmediatamente al iniciar.
	updateFileList()
  	for range ticker.C {
		updateFileList()
	}
}

func readInput(ch chan<- string) {

  scanner := bufio.NewScanner(os.Stdin)
  for scanner.Scan() {
    text := scanner.Text()
    ch <- text
  }
}


// updateFileList escanea la carpeta y actualiza la lista de archivos (sin bloqueo por consola)
// - Nuevos archivos: se agregan con Publish=false, TTL=config.TTL y LastSeen=now.
// - Archivos ausentes: se mantienen hasta que superen su TTL desde el último LastSeen.
func updateFileList() {
	mutex.Lock()
	defer mutex.Unlock()

	// Archivos a ignorar
	filesToIgnore := map[string]bool{
		"config.json":      true,
		"file_monitor.log": true,
		"main.go":          true,
		// agrega aquí el nombre del binario si compilas en la misma carpeta, p. ej. "mi_programa" o "mi_programa.exe"
	}

	log.Println("[+] Escaneando carpeta...")

	// Índice de los que ya tenemos en config
	existing := make(map[string]int) // nombre -> índice en config.Files
	for i, f := range config.Files {
		existing[f.Name] = i
	}

	// Listado actual del directorio
	entries, err := ioutil.ReadDir(config.WatchFolder)
	if err != nil {
		log.Printf("[!] Error leyendo carpeta %s: %v", config.WatchFolder, err)
		return
	}

	// Presencia actual (nombre -> true)
	present := make(map[string]bool)

	now := time.Now()

	// Detectar nuevos y refrescar LastSeen de existentes
	for _, e := range entries {
		if e.IsDir() || filesToIgnore[e.Name()] {
			continue
		}

		name := e.Name()
		present[name] = true

		if idx, ok := existing[name]; ok {
			// existente: refrescar LastSeen (sigue estando en disco)
			f := config.Files[idx]
			f.LastSeen = now
			config.Files[idx] = f
		} else {
			// nuevo: sin bloqueo por consola, entra como no publicable por defecto
			newFile := FileInfo{
				Name:      name,
				Extension: filepath.Ext(name),
				Publish:   false,
				TTL:       config.TTL,
				LastSeen:  now,
			}
			config.Files = append(config.Files, newFile)
			log.Printf("[+] Nuevo archivo detectado: %s (publish=false, ttl=%ds)", newFile.Name, newFile.TTL)
		}
	}

	// Filtrar eliminados por TTL: si ya no está en disco y venció su TTL desde el último LastSeen, lo quitamos
	var kept []FileInfo
	for _, f := range config.Files {
		if present[f.Name] {
			kept = append(kept, f)
			continue
		}
		// No está presente: conservar hasta que venza su TTL
		age := time.Since(f.LastSeen)
		if age < time.Duration(f.TTL)*time.Second {
			kept = append(kept, f)
		} else {
			log.Printf("[!] Expiró TTL o fue removido definitivamente: %s (age=%s, ttl=%ds)", f.Name, age, f.TTL)
		}
	}
	config.Files = kept

	if err := saveConfig(); err != nil {
		log.Printf("[!] Error guardando config: %v", err)
	}
	log.Println("[+] Escaneo completo.")
}


// udpServer inicia un servidor que escucha en el puerto 50000.
// Responde "ACK" solo si el archivo existe en la lista, está marcado como publicable
// y su TTL no ha expirado; en otro caso responde "NACK".
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
				// válido solo si no ha expirado el TTL
				if now.Sub(f.LastSeen) < time.Duration(f.TTL)*time.Second {
					response = "ACK"
				} else {
					log.Printf("[!] TTL expirado para '%s' (lastSeen=%s, ttl=%ds)", f.Name, f.LastSeen.Format(time.RFC3339), f.TTL)
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
		var folderPath string
		fmt.Print("[+] Escribe la ruta: ")
		fmt.Scanln(&folderPath)
		config.WatchFolder = folderPath
		saveConfig()
	}
	go fileMonitor()

	go udpServer()

	fmt.Println("[!] El programa se está ejecutando. Presiona CTRL+C para salir.")
	select {}
}
