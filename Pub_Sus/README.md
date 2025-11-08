# Publisher-Subscriber con gRPC en Go

Sistema de comunicación Publisher-Subscriber implementado con gRPC donde el servidor publica conjuntos de números en tres colas diferentes y los clientes se suscriben para procesarlos.

## Características

### Servidor
- Genera conjuntos de 2-3 números aleatorios
- Distribuye mensajes en tres colas: **principal**, **secundaria** y **terciaria**
- Soporta tres criterios de selección de cola:
  - **aleatorio**: 33% de probabilidad para cada cola
  - **ponderado**: 50% principal, 30% secundaria, 20% terciaria
  - **condicional**: basado en cantidad de números pares/impares
    - Dos números pares → cola principal
    - Dos números impares → cola secundaria
    - Tres números pares o impares → cola terciaria
- Recolecta resultados de clientes
- Se detiene al alcanzar 1 millón de resultados
- Imprime reporte final con suma total y estadísticas de clientes

### Cliente
- 50% probabilidad de suscribirse a 1 cola
- 50% probabilidad de suscribirse a 2 colas diferentes
- Cuando se suscribe a 2 colas, recibe mensajes aleatoriamente de cualquiera
- Procesa los números (suma) y envía el resultado al servidor

## Requisitos

- Go 1.21 o superior
- Protocol Buffers compiler (protoc)
- Plugins de Go para protoc:
  ```bash
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  ```

## Compilación

1. Generar código gRPC desde proto:
```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/pubsub.proto
```

2. Descargar dependencias:
```bash
go mod download
```

3. Compilar servidor:
```bash
go build -o bin/server server/server.go
```

4. Compilar cliente:
```bash
go build -o bin/client client/client.go
```

## Uso

### Iniciar el servidor

```bash
# Criterio aleatorio (por defecto)
./bin/server -port 50051 -criteria aleatorio

# Criterio ponderado
./bin/server -port 50051 -criteria ponderado

# Criterio condicional
./bin/server -port 50051 -criteria condicional
```

Parámetros:
- `-port`: Puerto del servidor (default: 50051)
- `-criteria`: Criterio de selección de cola (aleatorio, ponderado, condicional)

### Iniciar clientes

```bash
# Cliente 1
./bin/client -id 1 -server localhost:50051

# Cliente 2
./bin/client -id 2 -server localhost:50051

# Cliente N
./bin/client -id N -server localhost:50051
```

Parámetros:
- `-id`: Identificador único del cliente
- `-server`: Dirección del servidor (default: localhost:50051)

## Ejemplo de ejecución

Terminal 1 (Servidor):
```bash
$ ./bin/server -criteria ponderado
2025/11/08 13:00:00 Starting server on port 50051 with criteria: ponderado
2025/11/08 13:00:00 Server listening at [::]:50051
2025/11/08 13:00:05 Client 1 subscribed to queues: [primary]
2025/11/08 13:00:06 Client 2 subscribed to queues: [secondary tertiary]
2025/11/08 13:00:10 Received result from client 1: 150 (Total results: 1)
...
```

Terminal 2 (Cliente 1):
```bash
$ ./bin/client -id 1
2025/11/08 13:00:05 Client 1 connected to server at localhost:50051
2025/11/08 13:00:05 Client 1 subscribing to 1 queue: [primary]
2025/11/08 13:00:05 Client 1 started receiving messages
2025/11/08 13:00:10 Client 1 received msg 123 from queue primary: [45 55 50] -> result: 150
...
```

Terminal 3 (Cliente 2):
```bash
$ ./bin/client -id 2
2025/11/08 13:00:06 Client 2 connected to server at localhost:50051
2025/11/08 13:00:06 Client 2 subscribing to 2 queues: [secondary tertiary]
2025/11/08 13:00:06 Client 2 started receiving messages
2025/11/08 13:00:11 Client 2 received msg 124 from queue secondary: [30 45] -> result: 75
...
```

## Reporte Final

Cuando el servidor alcanza 1 millón de resultados, imprime:

```
=== FINAL REPORT ===
Total de resultados: 1000000
Suma total de resultados: 148523450

Clientes que trabajaron:
  - Cliente 1: 250000 resultados, suscrito a colas: [primary]
  - Cliente 2: 350000 resultados, suscrito a colas: [secondary tertiary]
  - Cliente 3: 400000 resultados, suscrito a colas: [primary secondary]
====================
```

## Estructura del proyecto

```
Pub_Sus/
├── proto/
│   ├── pubsub.proto          # Definición de servicios gRPC
│   ├── pubsub.pb.go          # Código generado
│   └── pubsub_grpc.pb.go     # Código generado
├── server/
│   └── server.go             # Implementación del servidor
├── client/
│   └── client.go             # Implementación del cliente
├── bin/                       # Binarios compilados
├── go.mod
└── README.md
```

## Criterios de selección explicados

### Aleatorio
Cada mensaje tiene exactamente 33.33% de probabilidad de ir a cualquiera de las tres colas.

### Ponderado
- 50% de mensajes → cola principal
- 30% de mensajes → cola secundaria
- 20% de mensajes → cola terciaria

### Condicional
Basado en la cantidad de números pares e impares:
- Si hay exactamente 2 números pares → cola principal
- Si hay exactamente 2 números impares → cola secundaria
- Si todos son pares o todos son impares (3 números) → cola terciaria
