#!/bin/bash

# Script para generar código de Protocol Buffers con verificación de PATH

echo "=== Generando código de Protocol Buffers ==="

# Obtener GOPATH y GOBIN
GOPATH=$(go env GOPATH)
GOBIN=$(go env GOBIN)

if [ -z "$GOBIN" ]; then
    GOBIN="$GOPATH/bin"
fi

echo "GOBIN: $GOBIN"

# Verificar si los plugins están instalados
if [ ! -f "$GOBIN/protoc-gen-go" ] || [ ! -f "$GOBIN/protoc-gen-go-grpc" ]; then
    echo "Instalando plugins de protoc..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Crear directorio proto si no existe
mkdir -p proto

# Generar código con PATH actualizado
echo "Generando código..."
PATH="$PATH:$GOBIN" protoc \
    --go_out=proto \
    --go_opt=paths=source_relative \
    --go-grpc_out=proto \
    --go-grpc_opt=paths=source_relative \
    calculator.proto

if [ $? -eq 0 ]; then
    echo "✓ Código generado exitosamente en ./proto/"
    ls -la proto/
else
    echo "✗ Error al generar código"
    echo ""
    echo "Posibles soluciones:"
    echo "1. Asegúrate de tener protoc instalado:"
    echo "   Ubuntu/Debian: sudo apt-get install -y protobuf-compiler"
    echo "   macOS: brew install protobuf"
    echo ""
    echo "2. Ejecuta: export PATH=\$PATH:$GOBIN"
    echo "3. Intenta de nuevo"
    exit 1
fi
