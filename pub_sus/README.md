# Publisher-Subscriber System

Sistema de publicación-suscripción con tres colas (principal, secundaria y terciaria) implementado en Go usando gRPC.

## Características

### Servidor
- **Tres colas**: Primary, Secondary, Tertiary
- **Criterios de selección configurables**:
  - `aleatorio`: 33% de probabilidad para cada cola
  - `ponderado`: 50% primary, 30% secondary, 20% tertiary
  - `condicional`: Basado en números pares/impares
    - 2 números pares → primary
    - 2 números impares → secondary
    - 3 números pares o impares → tertiary
- **Graceful shutdown**: Maneja señales SIGINT/SIGTERM correctamente
- **Thread-safe**: Sincronización apropiada para evitar race conditions
- **Reporte final**: Al alcanzar 1M resultados, imprime estadísticas completas

### Cliente
- **Auto-configuración de colas**: 50% probabilidad de suscribirse a 1 o 2 colas
- **Reconexión automática**: Exponential backoff hasta 30 segundos
- **Límites configurables**:
  - `--max-messages`: Número máximo de mensajes a procesar
  - `--duration`: Duración máxima de ejecución
- **Patrones de procesamiento**:
  - `fast`: 1ms de procesamiento
  - `normal`: 10ms de procesamiento
  - `slow`: 50ms de procesamiento
- **Graceful shutdown**: Maneja señales de cierre correctamente

## Compilación

```bash
# Generar código gRPC y compilar
bash build.sh

# O manualmente:
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/pubsub.proto

go build -o bin/server server.go
go build -o bin/client client.go
```

## Uso

### Servidor

```bash
# Iniciar con criterio aleatorio
./bin/server --criteria aleatorio

# Iniciar con criterio ponderado
./bin/server --criteria ponderado --port 50051

# Iniciar con criterio condicional
./bin/server --criteria condicional
```

### Cliente

```bash
# Cliente básico
./bin/client --id 1

# Cliente con límite de mensajes
./bin/client --id 2 --max-messages 1000

# Cliente con límite de tiempo (30 segundos)
./bin/client --id 3 --duration 30

# Cliente con procesamiento rápido
./bin/client --id 4 --pattern fast

# Cliente con procesamiento lento por 60 segundos
./bin/client --id 5 --pattern slow --duration 60

# Cliente completo
./bin/client --id 6 --server localhost:50051 --max-messages 5000 --pattern normal
```

## Testing

```bash
# Test del servidor
go test -v server.go server_test.go

# Test del cliente
go test -v client.go client_test.go

# Ver cobertura
go test -cover server.go server_test.go
go test -cover client.go client_test.go
```

## Arquitectura

```
┌─────────────────────────────────────────┐
│           PubSubServer                  │
│                                         │
│  ┌────────────┐  ┌────────────┐        │
│  │  Primary   │  │ Secondary  │        │
│  │   Queue    │  │   Queue    │        │
│  └────────────┘  └────────────┘        │
│         ┌────────────┐                  │
│         │  Tertiary  │                  │
│         │   Queue    │                  │
│         └────────────┘                  │
│                                         │
│  Selection Criteria:                    │
│  • Aleatorio (Random)                   │
│  • Ponderado (Weighted)                 │
│  • Condicional (Even/Odd)               │
└─────────────────────────────────────────┘
              │
              │ gRPC streaming
              │
    ┌─────────┴─────────┬─────────┐
    │                   │         │
┌───▼───┐          ┌────▼──┐  ┌──▼────┐
│Client1│          │Client2│  │Client3│
│ (1 Q) │          │ (2 Q) │  │ (1 Q) │
└───────┘          └───────┘  └───────┘
```

## Mejoras Implementadas

### Alta Prioridad ✅
1. **Race Condition Fixed**: Sincronización apropiada en `SendResult`
2. **Graceful Shutdown**: Servidor y clientes manejan señales correctamente
3. **Client Reconnection**: Reconexión automática con exponential backoff
4. **Unit Tests**: Tests completos para funciones críticas

### Cliente Mejorado ✅
- Flags de configuración (`--max-messages`, `--duration`, `--pattern`)
- Patrones de procesamiento (fast, normal, slow)
- Reconexión automática en caso de fallo
- Graceful shutdown con SIGINT/SIGTERM

## Criterio de Paro

El servidor detiene la publicación de números cuando:
1. Se alcanzan 1,000,000 de resultados, O
2. Se recibe una señal de cierre (SIGINT/SIGTERM)

Al detenerse, imprime:
- Total de resultados recibidos
- Suma total de todos los resultados
- Lista de clientes que trabajaron
- Colas a las que cada cliente estaba suscrito
- Cantidad de resultados por cliente

## Ejemplos de Ejecución

### Ejemplo 1: Prueba básica
```bash
# Terminal 1: Servidor con criterio aleatorio
./bin/server --criteria aleatorio

# Terminal 2-4: Tres clientes con diferentes configuraciones
./bin/client --id 1 --pattern fast
./bin/client --id 2 --pattern normal --max-messages 100
./bin/client --id 3 --pattern slow --duration 30
```

### Ejemplo 2: Prueba de estrés
```bash
# Terminal 1: Servidor con criterio ponderado
./bin/server --criteria ponderado

# Terminal 2: Iniciar 10 clientes rápidos
for i in {1..10}; do
  ./bin/client --id $i --pattern fast &
done
```

### Ejemplo 3: Prueba de reconexión
```bash
# Terminal 1: Servidor
./bin/server --criteria condicional

# Terminal 2: Cliente con reconexión
./bin/client --id 1

# En terminal 1: Presionar Ctrl+C y reiniciar el servidor
# El cliente se reconectará automáticamente
```

## Estructura del Proyecto

```
pub_sus/
├── server.go           # Implementación del servidor
├── server_test.go      # Tests del servidor
├── client.go           # Implementación del cliente
├── client_test.go      # Tests del cliente
├── pubsub.proto        # Definición de protobuf
├── proto/              # Código generado de protobuf
│   ├── pubsub.pb.go
│   └── pubsub_grpc.pb.go
├── build.sh            # Script de compilación
├── go.mod              # Dependencias de Go
└── README.md           # Esta documentación
```

## Dependencias

- Go 1.21+
- Protocol Buffers compiler (protoc)
- gRPC for Go
- google.golang.org/protobuf
- google.golang.org/grpc

## Troubleshooting

### Cliente no puede conectar
- Verificar que el servidor esté corriendo
- Verificar el puerto (default: 50051)
- El cliente intentará reconectarse automáticamente

### Servidor no compila
- Ejecutar `bash build.sh` para regenerar código protobuf
- Verificar que todas las dependencias estén instaladas

### Tests fallan
- Asegurarse de ejecutar tests de forma individual:
  - `go test -v server.go server_test.go`
  - `go test -v client.go client_test.go`
