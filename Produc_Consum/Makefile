.PHONY: proto build server client clean install-deps setup

# Variables
PROTO_DIR = proto
PROTO_FILE = calculator.proto
GOPATH := $(shell go env GOPATH)
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
	GOBIN := $(GOPATH)/bin
endif
export PATH := $(PATH):$(GOBIN)

# Setup completo del entorno
setup:
	@./setup.sh

# Instalar dependencias necesarias
install-deps:
	@echo "Instalando dependencias..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go mod tidy

# Generar código Go desde el archivo proto
proto:
	@echo "Generando código Go desde proto..."
	@mkdir -p $(PROTO_DIR)
	PATH=$(PATH):$(GOBIN) protoc --go_out=$(PROTO_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_FILE)

# Compilar servidor y cliente
build: proto
	@echo "Compilando servidor..."
	go build -o bin/server server.go
	@echo "Compilando cliente..."
	go build -o bin/client client.go

# Ejecutar el servidor
server: build
	@echo "Iniciando servidor gRPC..."
	./bin/server

# Ejecutar el cliente
client:
	@echo "Ejecutando cliente..."
	./bin/client

# Limpiar archivos generados
clean:
	@echo "Limpiando archivos generados..."
	rm -rf $(PROTO_DIR)/*.pb.go
	rm -rf bin/

# Ayuda
help:
	@echo "Comandos disponibles:"
	@echo "  make install-deps - Instalar dependencias necesarias"
	@echo "  make proto       - Generar código Go desde archivo proto"
	@echo "  make build       - Compilar servidor y cliente"
	@echo "  make server      - Ejecutar el servidor"
	@echo "  make client      - Ejecutar el cliente"
	@echo "  make clean       - Limpiar archivos generados"
