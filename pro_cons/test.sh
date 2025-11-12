#!/bin/bash

# Script para probar el sistema productor-consumidor con múltiples clientes

echo "Iniciando servidor..."
./server > server.log 2>&1 &
SERVER_PID=$!

# Esperar a que el servidor se inicie
sleep 2

echo "Iniciando 5 clientes..."

# Iniciar clientes con diferentes IDs
./client "Cliente-A" > client_A.log 2>&1 &
./client "Cliente-B" > client_B.log 2>&1 &
./client "Cliente-C" > client_C.log 2>&1 &
./client "Cliente-D" > client_D.log 2>&1 &
./client "Cliente-E" > client_E.log 2>&1 &

echo "Sistema iniciado!"
echo "Servidor PID: $SERVER_PID"
echo ""
echo "Para ver el progreso en tiempo real:"
echo "  tail -f server.log"
echo ""
echo "Para ver logs de un cliente específico:"
echo "  tail -f client_A.log"
echo ""
echo "El sistema se detendrá automáticamente después de 1 millón de resultados"
echo ""
echo "Para detener manualmente el sistema:"
echo "  kill $SERVER_PID"

# Esperar a que el servidor termine
wait $SERVER_PID

echo ""
echo "Servidor finalizado. Esperando a que los clientes terminen..."
sleep 5

echo "Prueba completada!"
