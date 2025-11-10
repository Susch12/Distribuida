#!/bin/bash

# Script para demostrar el cliente interactivo
# Este script envía comandos al cliente de forma automática

echo "=== Iniciando demostración del cliente interactivo ==="
echo ""
echo "Enviando expresiones al cliente..."
echo ""

# Usar heredoc para simular entrada del usuario
./bin/client <<EOF
ejemplos
3 + 5 * 2
100 / 4 + 10
2 ^ 4
15 - 5 * 2
ayuda
salir
EOF
