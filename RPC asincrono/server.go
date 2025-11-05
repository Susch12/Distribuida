package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

// Estructura del producto
type Product struct {
	ID     int     `xml:"id"`
	Name   string  `xml:"name"`
	Price  float64 `xml:"price"`
}

// Estructura para el archivo XML
type Products struct {
	XMLName  xml.Name  `xml:"products"`
	Products []Product `xml:"product"`
}

// Servidor con control de concurrencia
type ProductServer struct {
	mu            sync.RWMutex
	xmlFile       string
	insertQueue   chan *OperationRequest
	queryQueue    chan *OperationRequest
	pendingOps    map[string]chan int
	pendingOpsMu  sync.Mutex
}

// Estructura de solicitud
type OperationRequest struct {
	Product     Product
	Operation   string // "insert" o "query"
	CallbackURL string
	RequestID   string
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

// Respuesta inmediata
type Response struct {
	Message string
}

// Nueva instancia del servidor
func NewProductServer(xmlFile string) *ProductServer {
	server := &ProductServer{
		xmlFile:     xmlFile,
		insertQueue: make(chan *OperationRequest, 100),
		queryQueue:  make(chan *OperationRequest, 100),
		pendingOps:  make(map[string]chan int),
	}

	// Inicializar archivo XML si no existe
	if _, err := os.Stat(xmlFile); os.IsNotExist(err) {
		server.initXMLFile()
	}

	// Iniciar procesador de operaciones
	go server.processOperations()

	return server
}

// Inicializar archivo XML vacío
func (s *ProductServer) initXMLFile() {
	products := Products{
		Products: []Product{},
	}
	s.saveProducts(&products)
}

// Leer productos del archivo XML
func (s *ProductServer) loadProducts() (*Products, error) {
	xmlData, err := ioutil.ReadFile(s.xmlFile)
	if err != nil {
		return nil, err
	}

	var products Products
	err = xml.Unmarshal(xmlData, &products)
	if err != nil {
		return nil, err
	}

	return &products, nil
}

// Guardar productos en el archivo XML
func (s *ProductServer) saveProducts(products *Products) error {
	output, err := xml.MarshalIndent(products, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(s.xmlFile, output, 0644)
}

// Procesador de operaciones con prioridad
func (s *ProductServer) processOperations() {
	for {
		select {
		case insertOp := <-s.insertQueue:
			// Las inserciones tienen prioridad
			s.handleInsert(insertOp)
		default:
			// Si no hay inserciones, procesar consultas
			select {
			case insertOp := <-s.insertQueue:
				s.handleInsert(insertOp)
			case queryOp := <-s.queryQueue:
				s.handleQuery(queryOp)
			}
		}
	}
}

// Manejar inserción
func (s *ProductServer) handleInsert(req *OperationRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Printf("[INSERT] Procesando inserción de producto ID: %d\n", req.Product.ID)
	
	// Simular carga intensa (3 segundos)
	time.Sleep(3 * time.Second)

	products, err := s.loadProducts()
	if err != nil {
		log.Printf("Error al cargar productos: %v", err)
		s.sendCallback(req.CallbackURL, -1)
		return
	}

	// Verificar si el producto ya existe
	position := -1
	for i, p := range products.Products {
		if p.ID == req.Product.ID {
			position = i
			break
		}
	}

	if position != -1 {
		// Producto ya existe, no insertar
		fmt.Printf("[INSERT] Producto ID %d ya existe en posición %d\n", req.Product.ID, position)
		s.sendCallback(req.CallbackURL, position)
		return
	}

	// Insertar nuevo producto
	products.Products = append(products.Products, req.Product)
	err = s.saveProducts(products)
	if err != nil {
		log.Printf("Error al guardar productos: %v", err)
		s.sendCallback(req.CallbackURL, -1)
		return
	}

	position = len(products.Products) - 1
	fmt.Printf("[INSERT] Producto ID %d insertado en posición %d\n", req.Product.ID, position)
	s.sendCallback(req.CallbackURL, position)
}

// Manejar consulta
func (s *ProductServer) handleQuery(req *OperationRequest) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fmt.Printf("[QUERY] Consultando producto ID: %d\n", req.Product.ID)
	
	// Simular carga intensa (3 segundos)
	time.Sleep(3 * time.Second)

	products, err := s.loadProducts()
	if err != nil {
		log.Printf("Error al cargar productos: %v", err)
		s.sendCallback(req.CallbackURL, -1)
		return
	}

	// Buscar el producto
	position := -1
	for i, p := range products.Products {
		if p.ID == req.Product.ID {
			position = i
			fmt.Printf("[QUERY] Producto ID %d encontrado en posición %d\n", req.Product.ID, position)
			break
		}
	}

	if position == -1 {
		fmt.Printf("[QUERY] Producto ID %d no encontrado\n", req.Product.ID)
	}

	s.sendCallback(req.CallbackURL, position)
}

// Enviar callback al cliente
func (s *ProductServer) sendCallback(callbackURL string, position int) {
	client, err := rpc.DialHTTP("tcp", callbackURL)
	if err != nil {
		log.Printf("Error conectando al callback: %v", err)
		return
	}
	defer client.Close()

	var reply string
	err = client.Call("ClientCallback.ReceiveResult", position, &reply)
	if err != nil {
		log.Printf("Error llamando al callback: %v", err)
	}
}

// Método RPC para insertar producto
func (s *ProductServer) InsertProduct(args *InsertArgs, reply *Response) error {
	req := &OperationRequest{
		Product:     args.Product,
		Operation:   "insert",
		CallbackURL: args.CallbackURL,
	}

	s.insertQueue <- req
	reply.Message = "Solicitud de inserción recibida"
	fmt.Printf("Solicitud de inserción recibida para producto ID: %d\n", args.Product.ID)
	return nil
}

// Método RPC para consultar producto
func (s *ProductServer) QueryProduct(args *QueryArgs, reply *Response) error {
	req := &OperationRequest{
		Product: Product{
			ID: args.ProductID,
		},
		Operation:   "query",
		CallbackURL: args.CallbackURL,
	}

	s.queryQueue <- req
	reply.Message = "Solicitud de consulta recibida"
	fmt.Printf("Solicitud de consulta recibida para producto ID: %d\n", args.ProductID)
	return nil
}

func main() {
	// Crear servidor de productos
	productServer := NewProductServer("products.xml")

	// Registrar el servidor RPC
	rpc.Register(productServer)
	rpc.HandleHTTP()

	// Escuchar en el puerto 8000
	listener, err := net.Listen("tcp", ":8000")
	if err != nil {
		log.Fatal("Error iniciando servidor:", err)
	}

	fmt.Println("Servidor RPC ejecutándose en puerto 8000...")
	fmt.Println("Esperando conexiones de clientes...")
	http.Serve(listener, nil)
}
