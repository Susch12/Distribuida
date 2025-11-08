#!/bin/bash

echo "Building Publisher-Subscriber system..."

# Create bin directory
mkdir -p bin

# Generate gRPC code
echo "Generating gRPC code from protobuf..."
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/pubsub.proto

if [ $? -ne 0 ]; then
    echo "Error generating gRPC code"
    exit 1
fi

# Download dependencies
echo "Downloading dependencies..."
go mod download

# Build server
echo "Building server..."
go build -o bin/server server/server.go

if [ $? -ne 0 ]; then
    echo "Error building server"
    exit 1
fi

# Build client
echo "Building client..."
go build -o bin/client client/client.go

if [ $? -ne 0 ]; then
    echo "Error building client"
    exit 1
fi

echo "Build completed successfully!"
echo "Binaries are in the bin/ directory"
echo ""
echo "To run the server:"
echo "  ./bin/server -criteria aleatorio|ponderado|condicional"
echo ""
echo "To run a client:"
echo "  ./bin/client -id <client_id>"
