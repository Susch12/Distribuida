package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"time"
)

// Estructura del producto
type Product struct {
	ID    int
	Name  string
	Price float64
}

// Argumentos para las operaciones RPC
type InsertArgs struct {
	Product     Product
	CallbackURL string
}

type QueryArgs struct {
	ProductID   int
	CallbackURL string
}

// Respuesta del servidor
type Response struct {
	Message string
}

// Servidor de callback del cliente
type ClientCallback struct {
	clientID string
}

// Método para recibir el resultado del servidor
func (c *ClientCallback) ReceiveResult(position int, reply *string) error {
	timestamp := time.Now().Format("15:04:05")
	if position == -1 {
		fmt.Printf("[%s] [Cliente %s] Producto no encontrado o error en operación\n", 
			timestamp, c.clientID)
	} else {
		fmt.Printf("[%s] [Cliente %s] Operación exitosa - Posición en XML: %d\n", 
			timestamp, c.clientID, position)
	}
	*reply = "Callback recibido"
	return nil
}

// Cliente RPC
type RPCClient struct {
	serverAddr  string
	callbackURL string
	clientID    string
	callback    *ClientCallback
}

// Crear nuevo cliente
func NewRPCClient(serverAddr string, callbackPort int, clientID string) *RPCClient {
	client := &RPCClient{
		serverAddr:  serverAddr,
		callbackURL: fmt.Sprintf("localhost:%d", callbackPort),
		clientID:    clientID,
		callback:    &ClientCallback{clientID: clientID},
	}

	// Iniciar servidor de callback
	go client.startCallbackServer(callbackPort)
	time.Sleep(500 * time.Millisecond) // Esperar a que el servidor esté listo

	return client
}

// Iniciar servidor de callback
func (c *RPCClient) startCallbackServer(port int) {
	rpc.Register(c.callback)
	rpc.HandleHTTP()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal("Error iniciando servidor de callback:", err)
	}

	fmt.Printf("[Cliente %s] Servidor de callback iniciado en puerto %d\n", c.clientID, port)
	http.Serve(listener, nil)
}

// Insertar producto
func (c *RPCClient) insertProduct(product Product) {
	client, err := rpc.DialHTTP("tcp", c.serverAddr)
	if err != nil {
		log.Printf("Error conectando al servidor: %v", err)
		return
	}
	defer client.Close()

	args := &InsertArgs{
		Product:     product,
		CallbackURL: c.callbackURL,
	}

	var reply Response
	err = client.Call("ProductServer.InsertProduct", args, &reply)
	if err != nil {
		log.Printf("Error en llamada RPC: %v", err)
		return
	}

	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] [Cliente %s]  INSERT enviado - Producto ID: %d, Nombre: %s, Precio: $%.2f\n",
		timestamp, c.clientID, product.ID, product.Name, product.Price)
}

// Consultar producto
func (c *RPCClient) queryProduct(productID int) {
	client, err := rpc.DialHTTP("tcp", c.serverAddr)
	if err != nil {
		log.Printf("Error conectando al servidor: %v", err)
		return
	}
	defer client.Close()

	args := &QueryArgs{
		ProductID:   productID,
		CallbackURL: c.callbackURL,
	}

	var reply Response
	err = client.Call("ProductServer.QueryProduct", args, &reply)
	if err != nil {
		log.Printf("Error en llamada RPC: %v", err)
		return
	}

	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] [Cliente %s] QUERY enviado - Producto ID: %d\n",
		timestamp, c.clientID, productID)
}

// Generar nombre de producto aleatorio
func randomProductName() string {
	names := []string{
		"Laptop", "Mouse", "Teclado", "Monitor", "Audífonos",
		"Webcam", "Micrófono", "Tablet", "Smartphone", "Cargador",
		"Cable USB", "Disco Duro", "SSD", "RAM", "Procesador",
		"Tarjeta Gráfica", "Impresora", "Router", "Switch", "Hub",
	}
	return names[rand.Intn(len(names))]
}

// Generar precio aleatorio
func randomPrice() float64 {
	return float64(rand.Intn(9000)+1000) / 10.0 // Entre 100.0 y 1000.0
}

// Ejecutar operaciones aleatorias
func (c *RPCClient) runRandomOperations(numOperations int) {
	fmt.Printf("\n[Cliente %s] Iniciando %d operaciones aleatorias...\n", c.clientID, numOperations)
	fmt.Printf("[Cliente %s] =====================================\n", c.clientID)

	for i := 0; i < numOperations; i++ {
		// 60% probabilidad de inserción, 40% de consulta
		if rand.Float32() < 0.6 {
			product := Product{
				ID:    rand.Intn(50) + 1, // IDs entre 1 y 50
				Name:  randomProductName(),
				Price: randomPrice(),
			}
			c.insertProduct(product)
		} else {
			productID := rand.Intn(50) + 1
			c.queryProduct(productID)
		}

		// Espera aleatoria entre operaciones (0.5 a 2 segundos)
		time.Sleep(time.Duration(500+rand.Intn(1500)) * time.Millisecond)
	}

	fmt.Printf("\n[Cliente %s] Todas las operaciones enviadas. Esperando respuestas...\n", c.clientID)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Configuración
	serverAddr := "localhost:8000"
	callbackPort := 9000 // Cambiar para múltiples clientes
	clientID := "001"

	// Permitir configuración por argumentos (opcional)
	// Para ejecutar múltiples clientes, usar diferentes puertos
	// Ejemplo: go run client.go 9001 002

	// Crear cliente
	client := NewRPCClient(serverAddr, callbackPort, clientID)

	fmt.Printf("\n╔════════════════════════════════════════╗\n")
	fmt.Printf("║   CLIENTE RPC - ID: %s              ║\n", clientID)
	fmt.Printf("║   Servidor: %s           ║\n", serverAddr)
	fmt.Printf("║   Callback: puerto %d              ║\n", callbackPort)
	fmt.Printf("╚════════════════════════════════════════╝\n")

	// Esperar un momento antes de iniciar operaciones
	time.Sleep(1 * time.Second)

	// Ejecutar operaciones aleatorias
	numOperations := 10
	client.runRandomOperations(numOperations)

	// Mantener el cliente ejecutándose para recibir callbacks
	fmt.Printf("\n[Cliente %s] Esperando respuestas del servidor...\n", clientID)
	fmt.Printf("[Cliente %s] Presiona Ctrl+C para salir\n\n", clientID)
	
	// Esperar indefinidamente
	select {}
}
