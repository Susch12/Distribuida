#!/bin/bash

echo "=== Solución rápida para error de versiones ==="

# Eliminar archivos proto antiguos
echo "1. Limpiando archivos proto antiguos..."
rm -rf proto/*.pb.go

# Actualizar dependencias a versiones compatibles
echo "2. Actualizando dependencias..."
go get google.golang.org/grpc@v1.67.1
go get google.golang.org/protobuf@v1.35.1

# Limpiar y organizar módulos
echo "3. Organizando módulos..."
go mod tidy

# Regenerar proto
echo "4. Regenerando archivos proto..."
mkdir -p proto
PATH=$PATH:$(go env GOPATH)/bin protoc \
    --go_out=proto --go_opt=paths=source_relative \
    --go-grpc_out=proto --go-grpc_opt=paths=source_relative \
    calculator.proto

# Probar compilación
echo "5. Probando compilación..."
go build -o /tmp/test_server server.go && echo "✓ Servidor compila correctamente" && rm /tmp/test_server
go build -o /tmp/test_client client.go && echo "✓ Cliente compila correctamente" && rm /tmp/test_client

echo ""
echo "=== Listo! Ahora puedes ejecutar: ==="
echo "  go run server.go    # En una terminal"
echo "  go run client.go    # En otra terminal"
