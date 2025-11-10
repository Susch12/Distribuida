#!/bin/bash

echo "=== Configuración del entorno para gRPC con Go ==="

# Colores para output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Paso 1: Instalando plugins de protoc para Go...${NC}"

# Instalar protoc-gen-go y protoc-gen-go-grpc
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

echo -e "${GREEN}✓ Plugins instalados${NC}"

# Verificar que Go bin está en PATH
echo -e "${YELLOW}Paso 2: Verificando PATH...${NC}"

GOPATH=$(go env GOPATH)
GOBIN=$(go env GOBIN)

if [ -z "$GOBIN" ]; then
    GOBIN="$GOPATH/bin"
fi

echo "GOPATH: $GOPATH"
echo "GOBIN: $GOBIN"

# Agregar al PATH si no está
if [[ ":$PATH:" != *":$GOBIN:"* ]]; then
    echo -e "${YELLOW}Agregando $GOBIN al PATH...${NC}"
    export PATH=$PATH:$GOBIN
    echo -e "${GREEN}✓ PATH actualizado para esta sesión${NC}"
    echo ""
    echo -e "${YELLOW}Para hacer permanente el cambio, agrega esta línea a tu ~/.bashrc o ~/.zshrc:${NC}"
    echo "export PATH=\$PATH:$GOBIN"
else
    echo -e "${GREEN}✓ $GOBIN ya está en PATH${NC}"
fi

echo ""
echo -e "${YELLOW}Paso 3: Instalando dependencias de Go...${NC}"
go mod download
go mod tidy
echo -e "${GREEN}✓ Dependencias instaladas${NC}"

echo ""
echo -e "${YELLOW}Paso 4: Verificando instalación...${NC}"

# Verificar protoc
if command -v protoc &> /dev/null; then
    echo -e "${GREEN}✓ protoc instalado: $(protoc --version)${NC}"
else
    echo -e "${YELLOW}⚠ protoc no encontrado. Instálalo con:${NC}"
    echo "  Ubuntu/Debian: sudo apt-get install -y protobuf-compiler"
    echo "  macOS: brew install protobuf"
    echo "  O descarga desde: https://github.com/protocolbuffers/protobuf/releases"
fi

# Verificar protoc-gen-go
if command -v protoc-gen-go &> /dev/null || [ -f "$GOBIN/protoc-gen-go" ]; then
    echo -e "${GREEN}✓ protoc-gen-go instalado${NC}"
else
    echo -e "${YELLOW}⚠ protoc-gen-go no encontrado${NC}"
fi

# Verificar protoc-gen-go-grpc
if command -v protoc-gen-go-grpc &> /dev/null || [ -f "$GOBIN/protoc-gen-go-grpc" ]; then
    echo -e "${GREEN}✓ protoc-gen-go-grpc instalado${NC}"
else
    echo -e "${YELLOW}⚠ protoc-gen-go-grpc no encontrado${NC}"
fi

echo ""
echo -e "${GREEN}=== Configuración completada ===${NC}"
echo ""
echo "Ahora puedes ejecutar:"
echo "  make proto   # Para generar el código"
echo "  make build   # Para compilar todo"
echo ""
echo "Si aún tienes problemas, ejecuta:"
echo "  export PATH=\$PATH:$(go env GOPATH)/bin"
echo "  Y luego intenta de nuevo"
