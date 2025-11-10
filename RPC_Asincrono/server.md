# 📖 Explicación Detallada: server.go

## 🎯 Propósito del Servidor

El servidor es el **corazón del sistema**. Su trabajo es:
1. Recibir peticiones de múltiples clientes simultáneamente
2. Procesar operaciones sobre productos (INSERT y QUERY)
3. Mantener los datos en un archivo XML
4. Dar prioridad a las inserciones sobre las consultas
5. Notificar resultados a los clientes mediante callbacks

---

## 📦 Parte 1: Importaciones y Estructuras de Datos

```go
package main

import (
    "encoding/xml"      // Para trabajar con archivos XML
    "fmt"              // Para imprimir en consola
    "io/ioutil"        // Para leer/escribir archivos
    "log"              // Para registrar errores
    "net"              // Para conexiones de red
    "net/http"         // Para el servidor HTTP
    "net/rpc"          // Para RPC (llamadas remotas)
    "os"               // Para operaciones del sistema
    "sync"             // Para sincronización (mutexes)
    "time"             // Para sleep y timestamps
)
```

**¿Por qué estas librerías?**
- `encoding/xml`: Convertir structs de Go ↔ XML
- `net/rpc`: Permite que clientes remotos llamen funciones del servidor
- `sync`: Asegura que múltiples clientes no corrompan el XML
- `time`: Para simular la carga de 3 segundos

---

### 🏗️ Estructura: Product (Producto)

```go
type Product struct {
    ID     int     `xml:"id"`      // Identificador único
    Name   string  `xml:"name"`    // Nombre del producto
    Price  float64 `xml:"price"`   // Precio
}
```

**Explicación:**
- `type Product struct` → Define un nuevo tipo de dato llamado "Product"
- Los backticks `` `xml:"id"` `` → Le dicen a Go cómo convertir esto a XML
- Cuando guardamos un Product en XML, se ve así:
  ```xml
  <product>
      <id>1</id>
      <name>Laptop</name>
      <price>799.99</price>
  </product>
  ```

**Ejemplo práctico:**
```go
// Crear un producto en Go
laptop := Product{
    ID:    1,
    Name:  "Laptop",
    Price: 799.99,
}

// Go automáticamente lo puede convertir a XML gracias a los tags
```

---

### 🗂️ Estructura: Products (Colección de Productos)

```go
type Products struct {
    XMLName  xml.Name  `xml:"products"`   // Nombre del elemento raíz
    Products []Product `xml:"product"`    // Array de productos
}
```

**Explicación:**
- `[]Product` → Un slice (array dinámico) de productos
- `XMLName` → Define que el elemento raíz del XML se llama "products"
- Representa el archivo completo:
  ```xml
  <products>
      <product>...</product>
      <product>...</product>
  </products>
  ```

**¿Por qué necesitamos esto?**
Para poder leer y escribir el archivo XML completo de una sola vez.

---

### 🖥️ Estructura: ProductServer (El Servidor)

```go
type ProductServer struct {
    mu            sync.RWMutex              // 🔒 Candado para proteger el XML
    xmlFile       string                    // 📄 Ruta al archivo XML
    insertQueue   chan *OperationRequest    // 📥 Cola de inserciones (prioridad alta)
    queryQueue    chan *OperationRequest    // 📥 Cola de consultas (prioridad normal)
}
```

**Explicación detallada de cada campo:**

#### 1. `mu sync.RWMutex` - El Guardián del XML
```
Piensa en esto como un SEMÁFORO de tráfico para el archivo XML:

🔴 Lock()   → Luz roja para TODOS (escritura exclusiva)
🟢 RLock()  → Luz verde para LECTORES (múltiples lecturas simultáneas)
🔴 Unlock() → Quitar luz roja
🟢 RUnlock()→ Quitar luz verde

¿Por qué lo necesitamos?
─────────────────────────
Sin mutex:
Cliente 1: Lee XML → Producto ID=5 no existe
Cliente 2: Lee XML → Producto ID=5 no existe
Cliente 1: Inserta producto ID=5
Cliente 2: Inserta producto ID=5  ❌ DUPLICADO!

Con mutex:
Cliente 1: Lock() → Lee → Inserta → Unlock()
Cliente 2: (espera) → Lock() → Lee → Ya existe → Unlock() ✅ SIN DUPLICADO!
```

#### 2. `xmlFile string` - Ruta al Archivo
```go
xmlFile: "products.xml"
```
Simplemente guarda dónde está el archivo XML.

#### 3. `insertQueue chan *OperationRequest` - Cola de Inserciones
```
¿Qué es un "channel" (canal)?
─────────────────────────────
Es como una TUBERÍA donde pones cosas por un lado y las sacas por el otro.

Ejemplo visual:
┌─────────┐
│Cliente 1│──► [INSERT Laptop]   ─┐
└─────────┘                       │
                                  ├─► insertQueue ─► [Procesador]
┌─────────┐                       │
│Cliente 2│──► [INSERT Mouse]    ─┘
└─────────┘

El canal tiene buffer de 100, significa que puede almacenar hasta 100 
operaciones esperando ser procesadas.
```

#### 4. `queryQueue chan *OperationRequest` - Cola de Consultas
Similar a insertQueue pero para consultas (búsquedas).

---

### 📨 Estructura: OperationRequest (Solicitud de Operación)

```go
type OperationRequest struct {
    Product     Product   // El producto a insertar/buscar
    Operation   string    // "insert" o "query"
    CallbackURL string    // Dónde enviar la respuesta
}
```

**Ejemplo práctico:**
```go
req := &OperationRequest{
    Product: Product{
        ID:    15,
        Name:  "Laptop",
        Price: 799.99,
    },
    Operation:   "insert",
    CallbackURL: "localhost:9000",  // Dirección del cliente
}
```

Esto es como un **paquete** que contiene:
- 📦 El producto
- 🏷️ Qué hacer con él (insertar o buscar)
- 📬 Dónde enviar el resultado

---

### 📤 Estructuras para RPC (Argumentos y Respuestas)

```go
// Argumentos para insertar
type InsertArgs struct {
    Product     Product
    CallbackURL string
}

// Argumentos para consultar
type QueryArgs struct {
    ProductID   int       // Solo necesitamos el ID para buscar
    CallbackURL string
}

// Respuesta inmediata del servidor
type Response struct {
    Message string        // "Solicitud recibida"
}
```

**¿Por qué necesitamos estas estructuras?**
RPC en Go requiere que los argumentos y respuestas estén en structs.

---

## 🔧 Parte 2: Funciones de Inicialización

### 🏗️ NewProductServer() - Constructor del Servidor

```go
func NewProductServer(xmlFile string) *ProductServer {
    server := &ProductServer{
        xmlFile:     xmlFile,
        insertQueue: make(chan *OperationRequest, 100),  // Canal con buffer 100
        queryQueue:  make(chan *OperationRequest, 100),
    }

    // Si el XML no existe, créalo vacío
    if _, err := os.Stat(xmlFile); os.IsNotExist(err) {
        server.initXMLFile()
    }

    // Iniciar el procesador en segundo plano
    go server.processOperations()

    return server
}
```

**Paso a paso:**

1. **Crear el servidor:**
   ```go
   server := &ProductServer{...}
   ```
   `&` significa que estamos creando un puntero al servidor.

2. **Crear los canales:**
   ```go
   make(chan *OperationRequest, 100)
   ```
   - `make()` → Crear un nuevo canal
   - `100` → Buffer de 100 elementos
   - Sin buffer, el canal bloquearía si nadie está escuchando

3. **Verificar si existe el XML:**
   ```go
   if _, err := os.Stat(xmlFile); os.IsNotExist(err) {
       server.initXMLFile()
   }
   ```
   - `os.Stat()` → Obtiene información del archivo
   - Si no existe, crear uno vacío

4. **Iniciar procesador:**
   ```go
   go server.processOperations()
   ```
   - `go` → Ejecutar en una goroutine (hilo ligero)
   - Esta función correrá en paralelo, procesando operaciones

---

### 📄 initXMLFile() - Crear XML Vacío

```go
func (s *ProductServer) initXMLFile() {
    products := Products{
        Products: []Product{},  // Array vacío
    }
    s.saveProducts(&products)
}
```

**Resultado:**
Crea un archivo `products.xml` vacío:
```xml
<products>
</products>
```

---

### 📖 loadProducts() - Leer el XML

```go
func (s *ProductServer) loadProducts() (*Products, error) {
    // 1. Leer el archivo completo
    xmlData, err := ioutil.ReadFile(s.xmlFile)
    if err != nil {
        return nil, err
    }

    // 2. Convertir XML → struct de Go
    var products Products
    err = xml.Unmarshal(xmlData, &products)
    if err != nil {
        return nil, err
    }

    return &products, nil
}
```

**Explicación visual:**

```
Archivo products.xml                Go struct
─────────────────────              ──────────────────
<products>                         Products{
  <product>                  →       Products: []Product{
    <id>1</id>              →           {ID: 1,
    <name>Laptop</name>     →            Name: "Laptop",
    <price>799.99</price>   →            Price: 799.99},
  </product>                →         }
</products>                        }

xml.Unmarshal() hace esta conversión automáticamente!
```

---

### 💾 saveProducts() - Guardar en XML

```go
func (s *ProductServer) saveProducts(products *Products) error {
    // 1. Convertir struct → XML con formato bonito
    output, err := xml.MarshalIndent(products, "", "  ")
    if err != nil {
        return err
    }

    // 2. Escribir al archivo
    return ioutil.WriteFile(s.xmlFile, output, 0644)
}
```

**¿Qué hace `MarshalIndent`?**
```go
// Sin indent (feo):
<products><product><id>1</id><name>Laptop</name></product></products>

// Con indent (bonito):
<products>
  <product>
    <id>1</id>
    <name>Laptop</name>
  </product>
</products>
```

---

## ⚙️ Parte 3: El Procesador - El Corazón del Sistema

### 🔄 processOperations() - El Loop Infinito

```go
func (s *ProductServer) processOperations() {
    for {  // Loop infinito
        select {
        case insertOp := <-s.insertQueue:
            // SIEMPRE revisa inserciones primero
            s.handleInsert(insertOp)
        default:
            // Si no hay inserciones, revisar ambas colas
            select {
            case insertOp := <-s.insertQueue:
                s.handleInsert(insertOp)
            case queryOp := <-s.queryQueue:
                s.handleQuery(queryOp)
            }
        }
    }
}
```

**Explicación del sistema de prioridades:**

```
┌─────────────────────────────────────────────────────────┐
│               CÓMO FUNCIONA EL SELECT                   │
└─────────────────────────────────────────────────────────┘

Iteración 1:
  insertQueue: [INSERT A, INSERT B]
  queryQueue:  [QUERY 1, QUERY 2]
  
  Primer select:
    case insertOp := <-s.insertQueue:  ✅ HAY INSERT
      → Procesa INSERT A

Iteración 2:
  insertQueue: [INSERT B]
  queryQueue:  [QUERY 1, QUERY 2]
  
  Primer select:
    case insertOp := <-s.insertQueue:  ✅ HAY INSERT
      → Procesa INSERT B

Iteración 3:
  insertQueue: []  (vacía)
  queryQueue:  [QUERY 1, QUERY 2]
  
  Primer select:
    case insertOp := <-s.insertQueue:  ❌ VACÍA
    default:  ← Entra aquí
      Segundo select:
        case insertOp := <-s.insertQueue:  ❌ VACÍA
        case queryOp := <-s.queryQueue:   ✅ HAY QUERY
          → Procesa QUERY 1
```

**Clave:** Las inserciones SIEMPRE se procesan antes que las consultas.

---

### 📝 handleInsert() - Procesar una Inserción

```go
func (s *ProductServer) handleInsert(req *OperationRequest) {
    // 1. BLOQUEAR para escritura exclusiva
    s.mu.Lock()
    defer s.mu.Unlock()  // Desbloquear al terminar (aunque haya error)

    fmt.Printf("[INSERT] Procesando producto ID: %d\n", req.Product.ID)
    
    // 2. Simular carga intensa (3 segundos)
    time.Sleep(3 * time.Second)

    // 3. Cargar productos del XML
    products, err := s.loadProducts()
    if err != nil {
        log.Printf("Error al cargar productos: %v", err)
        s.sendCallback(req.CallbackURL, -1)
        return
    }

    // 4. Verificar si ya existe
    position := -1
    for i, p := range products.Products {
        if p.ID == req.Product.ID {
            position = i
            break
        }
    }

    if position != -1 {
        // Ya existe, no insertar
        fmt.Printf("[INSERT] Producto ID %d ya existe en posición %d\n", 
                   req.Product.ID, position)
        s.sendCallback(req.CallbackURL, position)
        return
    }

    // 5. Insertar nuevo producto
    products.Products = append(products.Products, req.Product)
    
    // 6. Guardar en XML
    err = s.saveProducts(products)
    if err != nil {
        log.Printf("Error al guardar: %v", err)
        s.sendCallback(req.CallbackURL, -1)
        return
    }

    // 7. Enviar resultado
    position = len(products.Products) - 1
    fmt.Printf("[INSERT] Producto ID %d insertado en posición %d\n", 
               req.Product.ID, position)
    s.sendCallback(req.CallbackURL, position)
}
```

**Paso a paso detallado:**

#### Paso 1: Bloquear (Lock)
```go
s.mu.Lock()
defer s.mu.Unlock()
```

**¿Qué hace `defer`?**
```
defer significa "ejecutar esto AL FINAL, pase lo que pase"

Ejemplo:
func hacerAlgo() {
    mu.Lock()
    defer mu.Unlock()  ← Se ejecutará al final
    
    // Si hay un error aquí...
    if err != nil {
        return  ← Unlock() SE EJECUTA ANTES de salir
    }
    
    // O si termina normal...
    fmt.Println("OK")  ← Unlock() SE EJECUTA después de esto
}

Sin defer, tendríamos que poner Unlock() en CADA return. ¡Tedioso y propenso a errores!
```

#### Paso 2: Sleep
```go
time.Sleep(3 * time.Second)
```
Simula que el servidor está haciendo un trabajo pesado.

#### Paso 3-4: Verificar Duplicados
```go
for i, p := range products.Products {
    if p.ID == req.Product.ID {
        position = i
        break
    }
}
```

**Ejemplo visual:**
```
products.Products = [
    {ID: 5, Name: "Mouse"},     ← posición 0
    {ID: 10, Name: "Teclado"},  ← posición 1
    {ID: 15, Name: "Monitor"},  ← posición 2
]

Queremos insertar: {ID: 10, Name: "Laptop"}

Loop:
i=0: p.ID(5) == 10? No, continuar
i=1: p.ID(10) == 10? ¡Sí! position = 1, break

Resultado: Ya existe en posición 1, NO insertar
```

#### Paso 5: Insertar
```go
products.Products = append(products.Products, req.Product)
```

`append()` agrega al final del slice.

#### Paso 6-7: Guardar y Notificar
Guarda el XML y envía el resultado al cliente.

---

### 🔍 handleQuery() - Procesar una Consulta

```go
func (s *ProductServer) handleQuery(req *OperationRequest) {
    // 1. BLOQUEAR para lectura (permite múltiples lectores)
    s.mu.RLock()
    defer s.mu.RUnlock()

    fmt.Printf("[QUERY] Consultando producto ID: %d\n", req.Product.ID)
    
    // 2. Simular carga (3 segundos)
    time.Sleep(3 * time.Second)

    // 3. Cargar productos
    products, err := s.loadProducts()
    if err != nil {
        log.Printf("Error: %v", err)
        s.sendCallback(req.CallbackURL, -1)
        return
    }

    // 4. Buscar el producto
    position := -1
    for i, p := range products.Products {
        if p.ID == req.Product.ID {
            position = i
            break
        }
    }

    // 5. Notificar resultado
    if position == -1 {
        fmt.Printf("[QUERY] Producto ID %d no encontrado\n", req.Product.ID)
    } else {
        fmt.Printf("[QUERY] Producto ID %d encontrado en posición %d\n", 
                   req.Product.ID, position)
    }
    
    s.sendCallback(req.CallbackURL, position)
}
```

**Diferencia clave con handleInsert:**
```go
// INSERT usa Lock (exclusivo)
s.mu.Lock()

// QUERY usa RLock (compartido)
s.mu.RLock()
```

**¿Por qué?**
```
Escenario 1: Múltiples QUERIES simultáneas
┌────────┐  ┌────────┐  ┌────────┐
│QUERY 1 │  │QUERY 2 │  │QUERY 3 │
└───┬────┘  └───┬────┘  └───┬────┘
    │           │           │
    └───────────┼───────────┘
                │
    Todas leen XML al mismo tiempo  ✅ SEGURO
    (solo leen, no modifican)

Escenario 2: INSERT + QUERY
┌────────┐  ┌────────┐
│ INSERT │  │ QUERY  │
└───┬────┘  └───┬────┘
    │           │
    │ Lock()    │ RLock()
    │           │ (ESPERA a que termine INSERT)
    │ modifica  │
    │ Unlock()  │
    │           ✓ Ahora sí puede leer
```

---

### 📞 sendCallback() - Enviar Resultado al Cliente

```go
func (s *ProductServer) sendCallback(callbackURL string, position int) {
    // 1. Conectar al servidor de callback del cliente
    client, err := rpc.DialHTTP("tcp", callbackURL)
    if err != nil {
        log.Printf("Error conectando al callback: %v", err)
        return
    }
    defer client.Close()

    // 2. Llamar al método remoto del cliente
    var reply string
    err = client.Call("ClientCallback.ReceiveResult", position, &reply)
    if err != nil {
        log.Printf("Error llamando al callback: %v", err)
    }
}
```

**¿Qué está pasando aquí?**

```
Servidor (este código)          Cliente (en su máquina)
─────────────────────          ───────────────────────
                               
1. Conectar a localhost:9000   ← Cliente tiene servidor RPC aquí
   rpc.DialHTTP()              
                               
2. Llamar función remota       → ClientCallback.ReceiveResult(position)
   client.Call()                  Esta función corre en el CLIENTE!
                               
                                  El cliente imprime:
                                  "✓ Posición: 0"
```

Es como hacer una **llamada telefónica** al cliente para decirle el resultado.

---

## 🎭 Parte 4: Métodos RPC (Interfaz Pública)

### 📥 InsertProduct() - Método RPC para Insertar

```go
func (s *ProductServer) InsertProduct(args *InsertArgs, reply *Response) error {
    // 1. Crear la solicitud
    req := &OperationRequest{
        Product:     args.Product,
        Operation:   "insert",
        CallbackURL: args.CallbackURL,
    }

    // 2. Poner en la cola
    s.insertQueue <- req
    
    // 3. Responder inmediatamente
    reply.Message = "Solicitud de inserción recibida"
    fmt.Printf("Solicitud de inserción recibida para producto ID: %d\n", 
               args.Product.ID)
    return nil
}
```

**Flujo completo:**

```
Cliente                      Servidor                    
───────                      ────────                    
                                                         
1. Llamar InsertProduct()                               
   ────────────────────────►                            
                                                         
                            2. Recibir args              
                            3. Crear OperationRequest    
                            4. Poner en insertQueue      
                            5. Responder "Recibido"      
   ◄────────────────────────                            
                                                         
Cliente continúa ejecutando...                          
(NO espera el procesamiento)                            
                                                         
                            [En paralelo]                
                            Processor goroutine:         
                            6. Toma de insertQueue       
                            7. Procesa (3 segundos)      
                            8. Guarda en XML             
                            9. Envía callback            
   ◄────────────────────────                            
   Recibe callback con                                  
   el resultado (posición)                              
```

**Clave:** El método retorna INMEDIATAMENTE, no espera los 3 segundos.

---

### 🔍 QueryProduct() - Método RPC para Consultar

```go
func (s *ProductServer) QueryProduct(args *QueryArgs, reply *Response) error {
    req := &OperationRequest{
        Product: Product{
            ID: args.ProductID,  // Solo necesitamos el ID
        },
        Operation:   "query",
        CallbackURL: args.CallbackURL,
    }

    s.queryQueue <- req
    reply.Message = "Solicitud de consulta recibida"
    fmt.Printf("Solicitud de consulta recibida para producto ID: %d\n", 
               args.ProductID)
    return nil
}
```

Similar a InsertProduct pero:
- Va a `queryQueue` (prioridad normal)
- Solo necesita el ID, no el producto completo

---

## 🚀 Parte 5: Main - Arrancar el Servidor

```go
func main() {
    // 1. Crear el servidor
    productServer := NewProductServer("products.xml")

    // 2. Registrar para RPC
    rpc.Register(productServer)
    rpc.HandleHTTP()

    // 3. Escuchar conexiones
    listener, err := net.Listen("tcp", ":8000")
    if err != nil {
        log.Fatal("Error iniciando servidor:", err)
    }

    // 4. Servir peticiones
    fmt.Println("Servidor RPC ejecutándose en puerto 8000...")
    fmt.Println("Esperando conexiones de clientes...")
    http.Serve(listener, nil)  // Bloquea aquí, sirviendo peticiones
}
```

**¿Qué hace cada línea?**

1. **Crear servidor:**
   ```go
   productServer := NewProductServer("products.xml")
   ```
   - Inicializa estructuras
   - Crea colas
   - Inicia procesador en goroutine

2. **Registrar:**
   ```go
   rpc.Register(productServer)
   ```
   - Le dice a Go RPC: "Estos métodos son llamables remotamente"
   - Go automáticamente expone `InsertProduct` y `QueryProduct`

3. **Escuchar:**
   ```go
   listener, err := net.Listen("tcp", ":8000")
   ```
   - Abre el puerto 8000
   - Espera conexiones TCP

4. **Servir:**
   ```go
   http.Serve(listener, nil)
   ```
   - Loop infinito que acepta conexiones
   - Cada cliente en su propia goroutine

---

## 🎬 Flujo Completo de una Operación INSERT

```
1. CLIENTE envía RPC
   ↓
2. SERVIDOR recibe en InsertProduct()
   - Crea OperationRequest
   - Pone en insertQueue
   - Retorna "Solicitud recibida"
   ↓
3. CLIENTE recibe respuesta y continúa
   ↓
4. PROCESADOR (goroutine) toma de insertQueue
   ↓
5. handleInsert() procesa:
   - Lock() → Bloquea XML
   - Sleep 3s → Simula carga
   - loadProducts() → Lee XML
   - Verifica duplicados
   - append() → Agrega producto
   - saveProducts() → Guarda XML
   - Unlock() → Libera XML
   ↓
6. sendCallback() envía resultado
   - Conecta al cliente
   - Llama ClientCallback.ReceiveResult(position)
   ↓
7. CLIENTE recibe callback
   - Imprime resultado
   - "✓ Posición: 0"
```

---

## 💡 Conceptos Clave para Explicar

### 1. ¿Por qué Goroutines?
```go
go server.processOperations()
```
- **Sin goroutine:** El servidor se bloquearía procesando, no aceptaría más clientes
- **Con goroutine:** El servidor puede aceptar peticiones MIENTRAS procesa

### 2. ¿Por qué Channels?
```go
s.insertQueue <- req  // Enviar a canal
op := <-s.insertQueue // Recibir de canal
```
- Comunicación segura entre goroutines
- Sin channels necesitaríamos mutexes más complejos

### 3. ¿Por qué RWMutex?
```go
Lock()   // Escritura exclusiva
RLock()  // Lectura compartida
```
- Permite múltiples lecturas simultáneas
- Escrituras son exclusivas
- Mejor rendimiento que Mutex simple

### 4. ¿Por qué el sistema de prioridades?
```go
select {
case insertOp := <-s.insertQueue:  // Revisa primero
    ...
default:  // Solo si no hay inserts
    ...
}
```
- Las inserciones son más importantes
- Asegura consistencia de datos
- Requisito del proyecto

---

## 📊 Resumen Visual del Servidor

```
┌───────────────────────────────────────────────────────┐
│                    SERVIDOR RPC                       │
│                    Puerto 8000                        │
├───────────────────────────────────────────────────────┤
│                                                       │
│  Métodos Públicos (RPC):                             │
│  ┌──────────────────────────────────────────────┐   │
│  │  InsertProduct(args, reply)                  │   │
│  │    → Encola en insertQueue                   │   │
│  │    → Retorna inmediatamente                  │   │
│  └──────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────┐   │
│  │  QueryProduct(args, reply)                   │   │
│  │    → Encola en queryQueue                    │   │
│  │    → Retorna inmediatamente                  │   │
│  └──────────────────────────────────────────────┘   │
│                                                       │
│  Sistema de Colas:                                   │
│  ┌─────────────────┐    ┌─────────────────┐         │
│  │  insertQueue    │    │   queryQueue    │         │
│  │  (buffer 100)   │    │   (buffer 100)  │         │
│  │  🔴🔴🔴         │    │   🔵🔵🔵       │         │
│  └────────┬────────┘    └────────┬────────┘         │
│           │                      │                   │
│           └──────────┬───────────┘                   │
│                      │                               │
│           ┌──────────▼──────────┐                    │
│           │  processOperations() │                    │
│           │   (goroutine)        │                    │
│           │  - Prioriza inserts  │                    │
│           │  - Procesa uno a uno │                    │
│           └──────────┬───────────┘                    │
│                      │                               │
│        ┌─────────────┴─────────────┐                 │
│        │                           │                 │
│        ▼                           ▼                 │
│  ┌──────────────┐         ┌──────────────┐          │
│  │handleInsert()│         │handleQuery() │          │
│  │- Lock()      │         │- RLock()     │          │
│  │- Sleep 3s    │         │- Sleep 3s    │          │
│  │- Verificar   │         │- Buscar      │          │
│  │- Insertar    │         │- Unlock()    │          │
│  │- Unlock()    │         │- Callback    │          │
│  │- Callback    │         └──────────────┘          │
│  └──────────────┘                                    │
│                                                       │
│  Almacenamiento:                                     │
│  ┌──────────────────────────────────────────────┐   │
│  │           products.xml                       │   │
│  │  <products>                                  │   │
│  │    <product><id>1</id>...</product>         │   │
│  │  </products>                                 │   │
│  │  🔒 Protegido por RWMutex                    │   │
│  └──────────────────────────────────────────────┘   │
│                                                       │
└───────────────────────────────────────────────────────┘
```

Este es el servidor completo. ¡Ahora veamos el cliente!
