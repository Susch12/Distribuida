package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
    "strings"
)

func main() {
    serverAddr := "127.0.0.1:50000" // Cambia si el servidor está en otra IP
    conn, err := net.Dial("udp", serverAddr)
    if err != nil {
        fmt.Println("Error al conectar con el servidor:", err)
        return
    }
    defer conn.Close()

    reader := bufio.NewReader(os.Stdin)
    fmt.Print("Ingrese el nombre del archivo (con extensión): ")
    filename, _ := reader.ReadString('\n')
    filename = strings.TrimSpace(filename)

    _, err = conn.Write([]byte(filename))
    if err != nil {
        fmt.Println("Error al enviar:", err)
        return
    }

    buf := make([]byte, 1024)
    n, err := conn.Read(buf)
    if err != nil {
        fmt.Println("Error al recibir respuesta:", err)
        return
    }

    response := string(buf[:n])
    fmt.Println("Respuesta del servidor:", response)
}
