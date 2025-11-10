#!/bin/bash

echo "=== Resolviendo dependencias y compilando ==="

# Colores
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Paso 1: Descargar dependencias
echo -e "${YELLOW}Paso 1: Descargando dependencias de Go...${NC}"
go mod download
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Dependencias descargadas${NC}"
else
    echo -e "${RED}✗ Error al descargar dependencias${NC}"
    exit 1
fi

# Paso 2: Organizar dependencias
echo -e "${YELLOW}Paso 2: Organizando dependencias con go mod tidy...${NC}"
go mod tidy
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Dependencias organizadas${NC}"
else
    echo -e "${RED}✗ Error al organizar dependencias${NC}"
    exit 1
fi

# Paso 3: Verificar que los archivos proto existen
echo -e "${YELLOW}Paso 3: Verificando archivos generados...${NC}"
if [ -f "proto/calculator.pb.go" ] && [ -f "proto/calculator_grpc.pb.go" ]; then
    echo -e "${GREEN}✓ Archivos proto encontrados${NC}"
    ls -la proto/
else
    echo -e "${RED}✗ Archivos proto no encontrados. Ejecuta primero: make proto${NC}"
    exit 1
fi

# Paso 4: Compilar servidor
echo -e "${YELLOW}Paso 4: Compilando servidor...${NC}"
mkdir -p bin
go build -o bin/server server.go
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Servidor compilado: bin/server${NC}"
else
    echo -e "${RED}✗ Error al compilar servidor${NC}"
    exit 1
fi

# Paso 5: Compilar cliente
echo -e "${YELLOW}Paso 5: Compilando cliente...${NC}"
go build -o bin/client client.go
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Cliente compilado: bin/client${NC}"
else
    echo -e "${RED}✗ Error al compilar cliente${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}=== ✓ Compilación exitosa ===${NC}"
echo ""
echo "Ahora puedes:"
echo "  1. Iniciar el servidor: ./bin/server"
echo "  2. En otra terminal, ejecutar el cliente: ./bin/client"
echo ""
echo "O usar Make:"
echo "  Terminal 1: make server"
echo "  Terminal 2: make client"
