# 📖 Explicación Detallada: client.go

## 🎯 Propósito del Cliente

El cliente es la **interfaz del usuario** con el sistema. Su trabajo es:
1. Generar operaciones aleatorias (INSERT y QUERY)
2. Enviarlas al servidor sin bloquearse
3. Ejecutar su propio servidor de callback
4. Recibir y mostrar los resultados del servidor

---

## 📦 Parte 1: Importaciones y Estructuras

```go
package main

import (
    "fmt"              // Para imprimir en consola
    "log"              // Para errores
    "math/rand"        // Para generar números aleatorios
    "net"              // Para conexiones de red
    "net/http"         // Para servidor HTTP
    "net/rpc"          // Para llamadas RPC
    "time"             // Para timestamps y sleep
)
```

**¿Por qué estas librerías?**
- `math/rand`: Para simular un usuario real con comportamiento aleatorio
- `net/rpc`: Para comunicarse con el servidor
- `time`: Para esperas entre operaciones y timestamps en logs

---

### 🏗️ Estructura: Product (Misma que en el servidor)

```go
type Product struct {
    ID    int
    Name  string
    Price float64
}
```

**Importante:** Esta estructura debe ser **idéntica** a la del servidor para que RPC funcione.

---

### 📤 Estructuras de Argumentos RPC

```go
type InsertArgs struct {
    Product     Product
    CallbackURL string
}

type QueryArgs struct {
    ProductID   int
    CallbackURL string
}

type Response struct {
    Message string
}
```

**También deben coincidir con el servidor** para que RPC pueda serializar/deserializar correctamente.

---

### 🎧 Estructura: ClientCallback (Servidor de Callback del Cliente)

```go
type ClientCallback struct {
    clientID string  // Identificador del cliente (ej: "001", "002")
}
```

**¿Por qué el cliente necesita un servidor?**
```
Flujo de comunicación bidireccional:

Cliente                           Servidor
───────                           ────────

1. Cliente → Servidor: "Inserta producto ID 15"
   (Cliente es CLIENTE RPC)

2. Servidor procesa... (3 segundos)

3. Servidor → Cliente: "Resultado: posición 0"
   (Cliente es SERVIDOR RPC para recibir callback)

El cliente es AMBOS:
- Cliente RPC (para enviar peticiones)
- Servidor RPC (para recibir callbacks)
```

---

### 🔔 Método: ReceiveResult() - Recibir Resultado del Servidor

```go
func (c *ClientCallback) ReceiveResult(position int, reply *string) error {
    timestamp := time.Now().Format("15:04:05")
    
    if position == -1 {
        fmt.Printf("[%s] [Cliente %s] ❌ Producto no encontrado o error en operación\n", 
            timestamp, c.clientID)
    } else {
        fmt.Printf("[%s] [Cliente %s] ✓ Operación exitosa - Posición en XML: %d\n", 
            timestamp, c.clientID, position)
    }
    
    *reply = "Callback recibido"
    return nil
}
```

**Explicación detallada:**

#### 1. Timestamp
```go
timestamp := time.Now().Format("15:04:05")
```
- `time.Now()`: Hora actual
- `Format("15:04:05")`: Formato HH:MM:SS
- Resultado: "14:30:45"

**¿Por qué este formato raro?**
En Go, el formato de tiempo usa una fecha de referencia: **"Mon Jan 2 15:04:05 MST 2006"**
- `15` = hora
- `04` = minuto
- `05` = segundo

Es como decir "muestra la parte de 15 horas, 04 minutos, 05 segundos de la fecha de referencia".

#### 2. Parámetro `position int`
El servidor envía:
- Un número **≥ 0** si la operación fue exitosa (posición en el XML)
- `-1` si hubo error o no se encontró

#### 3. Parámetro `reply *string`
Es un **puntero** a string que debemos llenar con nuestra respuesta.
```go
*reply = "Callback recibido"
```
El `*` significa "el valor al que apunta el puntero".

**Ejemplo visual:**
```
Memoria del servidor           Memoria del cliente
─────────────────────          ───────────────────

reply ────────────────────────► "Callback recibido"
(puntero)                       (valor)

Cuando hacemos *reply = "...", estamos modificando
la variable del SERVIDOR desde el CLIENTE
```

#### 4. Output en Consola
```
[14:30:45] [Cliente 001] ✓ Operación exitosa - Posición en XML: 0
[14:30:48] [Cliente 001] ❌ Producto no encontrado o error en operación
```

---

### 👤 Estructura: RPCClient (El Cliente Completo)

```go
type RPCClient struct {
    serverAddr  string           // "localhost:8000" (dirección del servidor)
    callbackURL string           // "localhost:9000" (dónde recibir callbacks)
    clientID    string           // "001", "002", etc.
    callback    *ClientCallback  // Servidor de callback
}
```

Esta estructura representa un **cliente completo** con:
- Información del servidor al que se conecta
- Su propia dirección para callbacks
- Su identificador único
- Su servidor de callbacks

---

## 🏗️ Parte 2: Construcción e Inicialización

### 🎬 NewRPCClient() - Constructor del Cliente

```go
func NewRPCClient(serverAddr string, callbackPort int, clientID string) *RPCClient {
    client := &RPCClient{
        serverAddr:  serverAddr,
        callbackURL: fmt.Sprintf("localhost:%d", callbackPort),
        clientID:    clientID,
        callback:    &ClientCallback{clientID: clientID},
    }

    // Iniciar servidor de callback en goroutine
    go client.startCallbackServer(callbackPort)
    
    // Esperar a que el servidor esté listo
    time.Sleep(500 * time.Millisecond)

    return client
}
```

**Paso a paso:**

#### 1. Crear la estructura
```go
client := &RPCClient{...}
```
Inicializa todos los campos del cliente.

#### 2. Formatear URL de callback
```go
callbackURL: fmt.Sprintf("localhost:%d", callbackPort)
```
Si `callbackPort = 9000`, el resultado es `"localhost:9000"`

#### 3. Iniciar servidor de callback
```go
go client.startCallbackServer(callbackPort)
```
**`go` = ejecutar en paralelo (goroutine)**

```
Sin go:                        Con go:
───────                        ───────

main()                         main()
  │                              │
  ├─► startCallbackServer()      ├─► go startCallbackServer()
  │      (BLOQUEA AQUÍ)          │      (se ejecuta en paralelo)
  │      nunca continúa          │
  ✗                              ├─► Continúa ejecutando
                                 ✓
```

#### 4. Esperar que el servidor esté listo
```go
time.Sleep(500 * time.Millisecond)
```
Damos 500ms para que el servidor de callback esté escuchando antes de enviar peticiones.

**¿Por qué?**
```
Timeline sin sleep:
─────────────────────────────────────────────
t=0ms:  go startCallbackServer()  (inicia)
t=1ms:  Enviar INSERT al servidor
t=10ms: Servidor intenta callback → ❌ No hay nadie escuchando

Timeline con sleep:
─────────────────────────────────────────────
t=0ms:   go startCallbackServer()  (inicia)
t=500ms: sleep termina
t=501ms: Servidor de callback ya está listo ✓
t=502ms: Enviar INSERT al servidor
t=3502ms: Servidor hace callback → ✓ Recibido!
```

---

### 🎧 startCallbackServer() - Iniciar Servidor de Callback

```go
func (c *RPCClient) startCallbackServer(port int) {
    // 1. Registrar el callback en RPC
    rpc.Register(c.callback)
    rpc.HandleHTTP()

    // 2. Escuchar en el puerto especificado
    listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        log.Fatal("Error iniciando servidor de callback:", err)
    }

    fmt.Printf("[Cliente %s] Servidor de callback iniciado en puerto %d\n", 
               c.clientID, port)
    
    // 3. Servir peticiones (BLOQUEA AQUÍ)
    http.Serve(listener, nil)
}
```

**¿Qué hace cada paso?**

#### 1. Registrar
```go
rpc.Register(c.callback)
```
Le dice a Go RPC: "El método `ClientCallback.ReceiveResult` puede ser llamado remotamente"

#### 2. Escuchar
```go
listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
```
Abre el puerto (ej: 9000) y espera conexiones.

**Formato del puerto:**
```go
fmt.Sprintf(":%d", 9000)  →  ":9000"
```
El `:` significa "todas las interfaces de red en este puerto"

#### 3. Servir
```go
http.Serve(listener, nil)
```
**Loop infinito** que acepta conexiones y procesa callbacks.
Por eso necesitamos `go` al llamar esta función.

---

## 📤 Parte 3: Enviar Operaciones al Servidor

### 📝 insertProduct() - Enviar Inserción

```go
func (c *RPCClient) insertProduct(product Product) {
    // 1. Conectar al servidor
    client, err := rpc.DialHTTP("tcp", c.serverAddr)
    if err != nil {
        log.Printf("Error conectando al servidor: %v", err)
        return
    }
    defer client.Close()

    // 2. Preparar argumentos
    args := &InsertArgs{
        Product:     product,
        CallbackURL: c.callbackURL,
    }

    // 3. Llamar método remoto
    var reply Response
    err = client.Call("ProductServer.InsertProduct", args, &reply)
    if err != nil {
        log.Printf("Error en llamada RPC: %v", err)
        return
    }

    // 4. Mostrar confirmación
    timestamp := time.Now().Format("15:04:05")
    fmt.Printf("[%s] [Cliente %s] 📤 INSERT enviado - Producto ID: %d, Nombre: %s, Precio: $%.2f\n",
        timestamp, c.clientID, product.ID, product.Name, product.Price)
}
```

**Explicación detallada:**

#### 1. Conectar al servidor
```go
client, err := rpc.DialHTTP("tcp", c.serverAddr)
```

**¿Qué es `DialHTTP`?**
```
Es como marcar un número de teléfono:

DialHTTP("tcp", "localhost:8000")
    │       │
    │       └─► Dirección (IP:Puerto)
    └─────────► Protocolo (TCP)

Resultado: Conexión establecida con el servidor
```

#### 2. `defer client.Close()`
```go
defer client.Close()
```
**Asegura que cerremos la conexión al terminar**, pase lo que pase.

Sin defer:
```go
client, _ := rpc.DialHTTP(...)
if err != nil {
    return  // ❌ CONEXIÓN QUEDA ABIERTA (memory leak)
}
// más código...
client.Close()
```

Con defer:
```go
client, _ := rpc.DialHTTP(...)
defer client.Close()  // ✓ Se cierra automáticamente al salir
if err != nil {
    return  // ✓ Conexión se cierra antes de salir
}
// más código...
// ✓ Conexión se cierra al terminar la función
```

#### 3. Preparar argumentos
```go
args := &InsertArgs{
    Product:     product,
    CallbackURL: c.callbackURL,
}
```
Empaqueta el producto y nuestra dirección de callback.

#### 4. Llamada RPC
```go
err = client.Call("ProductServer.InsertProduct", args, &reply)
```

**Desglose:**
- `"ProductServer.InsertProduct"`: Nombre del método en el servidor
  - `ProductServer` = nombre del tipo registrado
  - `InsertProduct` = nombre del método
- `args`: Los argumentos (se envían al servidor)
- `&reply`: Dónde guardar la respuesta del servidor

**¿Qué pasa internamente?**
```
Cliente (aquí)                          Servidor
──────────────                          ────────

1. Serializar args
   Product{ID:15...} → bytes

2. Enviar por red ─────────────────────►

                                        3. Recibir bytes
                                        4. Deserializar
                                           bytes → Product{ID:15...}
                                        5. Ejecutar InsertProduct()
                                        6. reply.Message = "Recibido"
                                        7. Serializar reply
                                           
                    ◄───────────────────── 8. Enviar respuesta

9. Deserializar
   bytes → Response{Message:"Recibido"}
10. Guardar en &reply
```

#### 5. Mostrar confirmación
```go
fmt.Printf("[%s] [Cliente %s] 📤 INSERT enviado...\n", ...)
```

**Salida:**
```
[14:30:45] [Cliente 001] 📤 INSERT enviado - Producto ID: 15, Nombre: Laptop, Precio: $799.50
```

---

### 🔍 queryProduct() - Enviar Consulta

```go
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
    fmt.Printf("[%s] [Cliente %s] 🔍 QUERY enviado - Producto ID: %d\n",
        timestamp, c.clientID, productID)
}
```

**Muy similar a `insertProduct()`, pero:**
- Solo envía el ID (no el producto completo)
- Llama a `QueryProduct` en vez de `InsertProduct`
- Usa emoji 🔍 en vez de 📤

---

## 🎲 Parte 4: Generación Aleatoria

### 📛 randomProductName() - Nombre Aleatorio

```go
func randomProductName() string {
    names := []string{
        "Laptop", "Mouse", "Teclado", "Monitor", "Audífonos",
        "Webcam", "Micrófono", "Tablet", "Smartphone", "Cargador",
        "Cable USB", "Disco Duro", "SSD", "RAM", "Procesador",
        "Tarjeta Gráfica", "Impresora", "Router", "Switch", "Hub",
    }
    return names[rand.Intn(len(names))]
}
```

**¿Cómo funciona?**

```go
rand.Intn(len(names))
```
- `len(names)` = 20 (cantidad de elementos)
- `rand.Intn(20)` = número aleatorio entre 0 y 19
- `names[número]` = selecciona ese elemento

**Ejemplo:**
```
names = ["Laptop", "Mouse", "Teclado", ...]
           0         1         2

rand.Intn(20) → 0 → "Laptop"
rand.Intn(20) → 15 → "Tarjeta Gráfica"
rand.Intn(20) → 2 → "Teclado"
```

---

### 💰 randomPrice() - Precio Aleatorio

```go
func randomPrice() float64 {
    return float64(rand.Intn(9000)+1000) / 10.0
}
```

**Paso a paso:**
```
1. rand.Intn(9000)  → número entre 0 y 8999
2. +1000            → número entre 1000 y 9999
3. float64(...)     → convertir a float64
4. / 10.0           → dividir entre 10

Ejemplo:
rand.Intn(9000) → 4567
4567 + 1000 = 5567
5567 / 10.0 = 556.7

Resultado: Precio entre $100.0 y $999.9
```

**¿Por qué dividir entre 10?**
Para tener decimales: $799.50 en lugar de $7995

---

### 🎰 runRandomOperations() - Generar Operaciones Aleatorias

```go
func (c *RPCClient) runRandomOperations(numOperations int) {
    fmt.Printf("\n[Cliente %s] 🚀 Iniciando %d operaciones aleatorias...\n", 
               c.clientID, numOperations)
    fmt.Printf("[Cliente %s] =====================================\n", c.clientID)

    for i := 0; i < numOperations; i++ {
        // 60% probabilidad de inserción, 40% de consulta
        if rand.Float32() < 0.6 {
            product := Product{
                ID:    rand.Intn(50) + 1,  // IDs entre 1 y 50
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

    fmt.Printf("\n[Cliente %s] ✅ Todas las operaciones enviadas. Esperando respuestas...\n", 
               c.clientID)
}
```

**Explicación detallada:**

#### 1. Loop de operaciones
```go
for i := 0; i < numOperations; i++ {
```
Repetir N veces (default: 10)

#### 2. Decisión aleatoria: INSERT o QUERY
```go
if rand.Float32() < 0.6 {
```

**¿Qué hace `rand.Float32()`?**
Genera un número decimal aleatorio entre 0.0 y 1.0

```
rand.Float32() puede ser:
0.234 → < 0.6 → ✓ INSERT
0.789 → < 0.6 → ✗ QUERY
0.512 → < 0.6 → ✓ INSERT
0.901 → < 0.6 → ✗ QUERY

Probabilidad:
60% de los casos < 0.6 → INSERT
40% de los casos >= 0.6 → QUERY
```

#### 3. Generar producto aleatorio (para INSERT)
```go
product := Product{
    ID:    rand.Intn(50) + 1,  // Entre 1 y 50
    Name:  randomProductName(),
    Price: randomPrice(),
}
```

**¿Por qué IDs entre 1 y 50?**
- Con 10 clientes haciendo 10 operaciones cada uno = 100 operaciones
- 60% inserciones = ~60 inserciones
- Rango de 50 IDs asegura **colisiones** (duplicados) para probar el sistema

**Ejemplo de colisión:**
```
Cliente 1 (t=0s):  INSERT ID=15 → Posición 0 ✓
Cliente 2 (t=1s):  INSERT ID=15 → Ya existe en posición 0 ✗
```

#### 4. ID aleatorio (para QUERY)
```go
productID := rand.Intn(50) + 1
c.queryProduct(productID)
```
Buscar un producto que **puede o no existir**.

#### 5. Espera aleatoria
```go
time.Sleep(time.Duration(500+rand.Intn(1500)) * time.Millisecond)
```

**Desglose:**
```go
rand.Intn(1500)           → 0 a 1499
500 + rand.Intn(1500)     → 500 a 1999
time.Duration(...)        → convertir a Duration
* time.Millisecond        → milisegundos

Resultado: Sleep entre 0.5 y 2 segundos
```

**¿Por qué espera aleatoria?**
Simula un usuario real que no envía operaciones instantáneamente.

---

## 🚀 Parte 5: Main - Arrancar el Cliente

```go
func main() {
    rand.Seed(time.Now().UnixNano())

    // Configuración
    serverAddr := "localhost:8000"
    callbackPort := 9000
    clientID := "001"

    // Crear cliente
    client := NewRPCClient(serverAddr, callbackPort, clientID)

    fmt.Printf("\n╔════════════════════════════════════════╗\n")
    fmt.Printf("║   CLIENTE RPC - ID: %s              ║\n", clientID)
    fmt.Printf("║   Servidor: %s           ║\n", serverAddr)
    fmt.Printf("║   Callback: puerto %d              ║\n", callbackPort)
    fmt.Printf("╚════════════════════════════════════════╝\n")

    // Esperar antes de iniciar
    time.Sleep(1 * time.Second)

    // Ejecutar operaciones aleatorias
    numOperations := 10
    client.runRandomOperations(numOperations)

    // Mantener el cliente ejecutándose
    fmt.Printf("\n[Cliente %s] Esperando respuestas del servidor...\n", clientID)
    fmt.Printf("[Cliente %s] Presiona Ctrl+C para salir\n\n", clientID)
    
    select {}  // Esperar indefinidamente
}
```

**Explicación de cada sección:**

### 1. Inicializar generador aleatorio
```go
rand.Seed(time.Now().UnixNano())
```

**¿Para qué?**
Sin esto, `rand` genera siempre la misma secuencia:
```
Sin seed:
Ejecución 1: [15, 23, 8, 42, ...]
Ejecución 2: [15, 23, 8, 42, ...]  ← ¡Mismo!
Ejecución 3: [15, 23, 8, 42, ...]  ← ¡Mismo!

Con seed (tiempo actual):
Ejecución 1: [15, 23, 8, 42, ...]
Ejecución 2: [31, 5, 19, 7, ...]   ← Diferente
Ejecución 3: [44, 12, 38, 2, ...]  ← Diferente
```

`UnixNano()` = nanosegundos desde 1 enero 1970 (siempre diferente)

### 2. Configuración
```go
serverAddr := "localhost:8000"
callbackPort := 9000
clientID := "001"
```

**Para múltiples clientes, cambiar:**
- Cliente 1: `callbackPort = 9000`, `clientID = "001"`
- Cliente 2: `callbackPort = 9001`, `clientID = "002"`
- Cliente 3: `callbackPort = 9002`, `clientID = "003"`

### 3. Crear cliente
```go
client := NewRPCClient(serverAddr, callbackPort, clientID)
```
Inicializa el cliente y su servidor de callback.

### 4. Banner informativo
```go
fmt.Printf("\n╔════════════════════════════════════════╗\n")
fmt.Printf("║   CLIENTE RPC - ID: %s              ║\n", clientID)
...
```
Muestra información visual del cliente.

### 5. Espera inicial
```go
time.Sleep(1 * time.Second)
```
Da tiempo al servidor de callback para estar completamente listo.

### 6. Ejecutar operaciones
```go
numOperations := 10
client.runRandomOperations(numOperations)
```
Genera y envía 10 operaciones aleatorias.

### 7. Esperar indefinidamente
```go
select {}
```

**¿Qué es `select {}`?**
```
select sin casos = BLOQUEO INFINITO

¿Por qué?
El cliente ha enviado todas sus peticiones, pero los callbacks
llegarán en el futuro (después de 3 segundos cada uno).

Si el programa termina, el servidor de callback se cierra y
no podremos recibir los resultados.

select {} mantiene el programa vivo para recibir callbacks.
```

**Alternativa (no recomendada):**
```go
for {
    time.Sleep(1 * time.Hour)  // Dormir para siempre
}
```

---

## 🎬 Flujo Completo de una Operación (Cliente)

```
1. main() inicia
   ↓
2. NewRPCClient()
   - Crear estructura del cliente
   - go startCallbackServer() → Inicia en goroutine
   - Sleep 500ms → Esperar que esté listo
   ↓
3. runRandomOperations(10)
   ↓
4. Loop (10 veces):
   ├─► 60%: insertProduct()
   │   - Conectar al servidor (DialHTTP)
   │   - Call("ProductServer.InsertProduct", ...)
   │   - Recibir respuesta inmediata "Recibido"
   │   - Imprimir "📤 INSERT enviado"
   │   - Cerrar conexión
   │
   └─► 40%: queryProduct()
       - Conectar al servidor
       - Call("ProductServer.QueryProduct", ...)
       - Recibir respuesta inmediata "Recibido"
       - Imprimir "🔍 QUERY enviado"
       - Cerrar conexión
   ↓
   Sleep aleatorio (0.5-2s)
   ↓
5. Todas las operaciones enviadas
   ↓
6. select {} → Esperar callbacks
   ↓
   [En paralelo, el servidor de callback recibe]
   ↓
7. ReceiveResult(position) llamado por el servidor
   - Imprimir "✓ Posición: X" o "❌ No encontrado"
   ↓
   [Se repite para cada operación]
```

---

## 💡 Conceptos Clave para Explicar

### 1. ¿Por qué el cliente es también un servidor?
```
Tradicional (síncrono):
Cliente: "Dame el resultado"
         (espera 3 segundos bloqueado)
Servidor: "Aquí está"

Con callbacks (asíncrono):
Cliente: "Procesa esto, avísame a localhost:9000"
         (continúa haciendo otras cosas)
Servidor: (3 segundos después)
          *llama a localhost:9000*
          "Aquí está el resultado"
Cliente: *recibe en ReceiveResult()*
```

### 2. ¿Por qué goroutines?
```go
go client.startCallbackServer(callbackPort)
```
- Sin `go`: El programa se bloquearía en `startCallbackServer()` y nunca enviaría operaciones
- Con `go`: El servidor de callback corre en paralelo mientras enviamos operaciones

### 3. ¿Por qué `defer client.Close()`?
- Garantiza que cerremos la conexión RPC
- Previene memory leaks
- Funciona incluso si hay errores

### 4. ¿Por qué IDs entre 1-50?
- Fuerza colisiones (inserciones duplicadas)
- Prueba el manejo de duplicados del servidor
- Hace consultas más interesantes (algunos encontrados, otros no)

### 5. ¿Por qué operaciones aleatorias?
- Simula comportamiento real de usuarios
- Prueba concurrencia del servidor
- 60/40 split asegura más inserciones (para tener datos que consultar)

---

## 📊 Resumen Visual del Cliente

```
┌────────────────────────────────────────────────────────┐
│                   CLIENTE RPC                          │
│                   ID: 001                              │
│                   Callback Port: 9000                  │
├────────────────────────────────────────────────────────┤
│                                                        │
│  ┌──────────────────────────────────────────────────┐ │
│  │           SERVIDOR DE CALLBACK                   │ │
│  │           (corre en goroutine)                   │ │
│  │                                                  │ │
│  │  ┌────────────────────────────────────────────┐ │ │
│  │  │  ReceiveResult(position int)               │ │ │
│  │  │    ← Llamado por el servidor principal    │ │ │
│  │  │    ← Imprime resultado                     │ │ │
│  │  └────────────────────────────────────────────┘ │ │
│  │                                                  │ │
│  │  Escucha en: localhost:9000                     │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  ┌──────────────────────────────────────────────────┐ │
│  │         GENERADOR DE OPERACIONES                 │ │
│  │                                                  │ │
│  │  for i := 0; i < 10; i++ {                     │ │
│  │    if random < 0.6 {                           │ │
│  │      insertProduct()  ────┐                    │ │
│  │    } else {                │                    │ │
│  │      queryProduct()  ──────┤                    │ │
│  │    }                       │                    │ │
│  │    sleep(random)           │                    │ │
│  │  }                         │                    │ │
│  └────────────────────────────┼────────────────────┘ │
│                               │                      │
│                               ▼                      │
│  ┌──────────────────────────────────────────────┐   │
│  │      CLIENTE RPC (envía peticiones)          │   │
│  │                                              │   │
│  │  insertProduct():                           │   │
│  │    1. Conectar a servidor (localhost:8000)  │   │
│  │    2. Call("ProductServer.InsertProduct")   │   │
│  │    3. Recibir ACK inmediato                 │   │
│  │    4. Cerrar conexión                       │   │
│  │                                              │   │
│  │  queryProduct():                            │   │
│  │    1. Conectar a servidor                   │   │
│  │    2. Call("ProductServer.QueryProduct")    │   │
│  │    3. Recibir ACK inmediato                 │   │
│  │    4. Cerrar conexión                       │   │
│  └──────────────────────────────────────────────┘   │
│                                                        │
│  ┌──────────────────────────────────────────────────┐ │
│  │            DATOS ALEATORIOS                      │ │
│  │                                                  │ │
│  │  IDs: 1-50                                      │ │
│  │  Nombres: ["Laptop", "Mouse", ...]             │ │
│  │  Precios: $100.0 - $999.9                      │ │
│  │  Sleep: 0.5s - 2.0s                            │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
└────────────────────────────────────────────────────────┘

Output de ejemplo:
──────────────────────────────────────────────────────────
╔════════════════════════════════════════╗
║   CLIENTE RPC - ID: 001              ║
║   Servidor: localhost:8000           ║
║   Callback: puerto 9000              ║
╚════════════════════════════════════════╝

[Cliente 001] Servidor de callback iniciado en puerto 9000

[Cliente 001] 🚀 Iniciando 10 operaciones aleatorias...
[Cliente 001] =====================================
[14:30:45] [Cliente 001] 📤 INSERT enviado - Producto ID: 15, Nombre: Laptop, Precio: $799.50
[14:30:48] [Cliente 001] ✓ Operación exitosa - Posición en XML: 0
[14:30:47] [Cliente 001] 🔍 QUERY enviado - Producto ID: 23
[14:30:50] [Cliente 001] ❌ Producto no encontrado o error en operación
...
```

---

## 🔗 Interacción Cliente ↔ Servidor

```
CLIENTE                                    SERVIDOR
─────────────────────────────────────────────────────────

1. main()
   └─► NewRPCClient()
       └─► go startCallbackServer()
           Puerto 9000 ✓ escuchando

2. runRandomOperations()
   └─► insertProduct(ID=15)
       ├─► DialHTTP("localhost:8000")
       └─► Call("InsertProduct")
           ────────────────────────────► ProductServer.InsertProduct()
                                         - Encolar en insertQueue
                                         - return "Recibido"
           ◄────────────────────────────
       "Recibido" ✓
       Close()

3. sleep(random)

4. queryProduct(ID=15)
       ├─► DialHTTP("localhost:8000")
       └─► Call("QueryProduct")
           ────────────────────────────► ProductServer.QueryProduct()
                                         - Encolar en queryQueue
                                         - return "Recibido"
           ◄────────────────────────────
       "Recibido" ✓
       Close()

5. select {} → Esperando...

                                         [Processor goroutine]
                                         ├─► handleInsert(ID=15)
                                         │   - Sleep 3s
                                         │   - Insertar en XML
                                         │   - posición = 0
                                         │
                                         └─► sendCallback()
                                             DialHTTP("localhost:9000")
                                             Call("ReceiveResult", 0)
           ◄────────────────────────────
   ReceiveResult(0)
   └─► Imprimir "✓ Posición: 0"

                                         ├─► handleQuery(ID=15)
                                         │   - Sleep 3s
                                         │   - Buscar en XML
                                         │   - encontrado en 0
                                         │
                                         └─► sendCallback()
                                             Call("ReceiveResult", 0)
           ◄────────────────────────────
   ReceiveResult(0)
   └─► Imprimir "✓ Posición: 0"
```

---

Este es el cliente completo. Junto con el servidor, forman un sistema distribuido completo de gestión de productos con RPC y callbacks asíncronos! 🎉
