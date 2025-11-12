# Sistema Productor-Consumidor con gRPC

Sistema distribuido que implementa el patrón productor-consumidor utilizando gRPC en Go.

## Descripción

El sistema genera vectores únicos de 3 números aleatorios (1-1000) y los distribuye a múltiples clientes a través de una cola. Los clientes procesan estos números aplicando una función matemática (suma) y devuelven los resultados al servidor. El sistema se detiene automáticamente después de recibir 1 millón de resultados.

## Requisitos Implementados

- ✅ Servicio gRPC que genera 3 números aleatorios (1-1000)
- ✅ Números almacenados en una cola (buffered channel)
- ✅ Vectores generados son únicos
- ✅ Múltiples clientes (n clientes) pueden conectarse simultáneamente
- ✅ Clientes toman números, aplican función matemática (suma) y devuelven resultado
- ✅ Servidor calcula la suma de todos los resultados
- ✅ Servidor mantiene histórico de resultados por cliente
- ✅ Sistema se detiene tras 1 millón de resultados

## Arquitectura

### Servidor (`server.go`)
- **Productor**: Genera vectores únicos de 3 números aleatorios en segundo plano
- **Cola**: Buffered channel con capacidad de 10,000 vectores
- **GetNumbers**: Endpoint para que clientes soliciten vectores
- **SubmitResult**: Endpoint para que clientes envíen resultados procesados
- **Estadísticas**: Tracking de:
  - Total de resultados recibidos
  - Suma de todos los resultados
  - Ranking de clientes por número de resultados procesados
  - Vectores únicos generados

### Cliente (`client.go`)
- **Conexión**: Se conecta al servidor vía gRPC
- **ID único**: Cada cliente tiene un identificador único
- **Procesamiento**:
  1. Solicita vectores al servidor
  2. Aplica función matemática (suma: num1 + num2 + num3)
  3. Envía resultado al servidor
- **Auto-terminación**: Se detiene cuando el servidor alcanza el límite

### Protocolo (`calculator.proto`)
```protobuf
service ProducerConsumer {
  rpc GetNumbers(NumberRequest) returns (NumberResponse);
  rpc SubmitResult(ResultRequest) returns (ResultResponse);
}
```

## Compilación

```bash
# Generar código desde proto (si es necesario)
export PATH=$PATH:$(go env GOPATH)/bin
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       calculator.proto
mv calculator.pb.go calculator_grpc.pb.go proto/

# Compilar servidor y cliente
go build -o server server.go
go build -o client client.go
```

## Uso

### Opción 1: Ejecución Manual

**Terminal 1 - Servidor:**
```bash
./server
```

**Terminal 2, 3, 4... - Clientes:**
```bash
./client "Cliente-A"
./client "Cliente-B"
./client "Cliente-C"
# ... más clientes según necesites
```

### Opción 2: Script de Prueba Automático

```bash
./test.sh
```

Este script inicia automáticamente:
- 1 servidor
- 5 clientes (Cliente-A, Cliente-B, Cliente-C, Cliente-D, Cliente-E)

Los logs se guardan en:
- `server.log` - Log del servidor
- `client_A.log`, `client_B.log`, etc. - Logs de cada cliente

### Monitoreo en Tiempo Real

```bash
# Ver progreso del servidor
tail -f server.log

# Ver progreso de un cliente específico
tail -f client_A.log
```

## Ejemplo de Salida

### Durante la Ejecución
```
2025/11/11 22:11:16 ======================================================================
2025/11/11 22:11:16 Servidor gRPC Productor-Consumidor iniciado en puerto 50051
2025/11/11 22:11:16 Generando vectores únicos de 3 números (1-1000)...
2025/11/11 22:11:16 Sistema se detendrá después de 1000000 resultados
2025/11/11 22:11:16 ======================================================================
2025/11/11 22:11:16 Productor: 10000 vectores únicos generados
2025/11/11 22:11:20 Progreso: 10000 resultados recibidos (1.00% completado)
2025/11/11 22:11:24 Progreso: 20000 resultados recibidos (2.00% completado)
```

### Estadísticas Finales
```
======================================================================
ESTADÍSTICAS FINALES DEL SISTEMA
======================================================================
Total de resultados recibidos: 1000000
Suma total de todos los resultados: 1500482397
Vectores únicos generados: 1000000

RANKING DE CLIENTES (por número de resultados resueltos):
----------------------------------------------------------------------
 1. Cliente-A            :   200543 resultados (20.05%)
 2. Cliente-C            :   200234 resultados (20.02%)
 3. Cliente-B            :   200102 resultados (20.01%)
 4. Cliente-D            :   199876 resultados (19.99%)
 5. Cliente-E            :   199245 resultados (19.92%)
======================================================================
```

## Características Técnicas

- **Concurrencia**: Uso de goroutines y channels para manejo concurrente
- **Sincronización**: Mutexes (RWMutex) para proteger estructuras compartidas
- **Vectores únicos**: Hash map para evitar duplicados
- **Comunicación**: gRPC con Protocol Buffers
- **Timeouts**: Timeouts en todas las operaciones de red
- **Manejo de errores**: Recuperación ante fallos con reintentos
- **Escalabilidad**: Soporta n clientes concurrentes

## Modificación de Parámetros

Puedes ajustar los siguientes parámetros en `server.go`:

```go
const (
    MAX_RESULTS       = 1000000  // Límite de resultados (cambia para pruebas rápidas)
    MIN_NUMBER        = 1        // Rango mínimo
    MAX_NUMBER        = 1000     // Rango máximo
    QUEUE_BUFFER_SIZE = 10000    // Tamaño de la cola
)
```

## Función Matemática

La función por defecto es la **suma** de los 3 números. Para cambiarla, modifica la función en `client.go`:

```go
func applyMathFunction(num1, num2, num3 int32) int32 {
    // Ejemplo: multiplicación
    return num1 * num2 * num3

    // Ejemplo: promedio
    return (num1 + num2 + num3) / 3
}
```

## Detener el Sistema

El sistema se detiene automáticamente al alcanzar 1 millón de resultados. Para detenerlo manualmente:

```bash
# Si usaste test.sh
pkill -f "./server"

# Si lo ejecutaste manualmente
# Presiona Ctrl+C en la terminal del servidor
```
