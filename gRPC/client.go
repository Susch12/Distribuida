package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	pb "calculator/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Obtener dirección del servidor (por defecto localhost:50051)
	serverAddr := "localhost:50051"
	if len(os.Args) > 1 {
		serverAddr = os.Args[1]
	} else if addr := os.Getenv("SERVER_ADDR"); addr != "" {
		serverAddr = addr
	}

	// Conectar al servidor
	fmt.Printf("Conectando a %s...\n", serverAddr)

	// Usar grpc.NewClient (reemplaza el deprecated grpc.Dial)
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Error al crear el cliente gRPC: %v\nVerifica que la dirección del servidor sea correcta.", err)
	}
	defer conn.Close()

	client := pb.NewCalculatorClient(conn)

	// Probar la conexión con un timeout
	fmt.Print("Verificando conectividad... ")
	testCtx, testCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer testCancel()

	// Hacer una llamada de prueba para verificar que el servidor responde
	testExpr := &pb.Expression{
		FirstNumber: 0,
		Operations:  []*pb.Operation{},
	}
	_, err = client.Evaluate(testCtx, testExpr)
	if err != nil {
		fmt.Println("✗")
		log.Fatalf("\nError: No se pudo conectar al servidor en %s\n"+
			"Posibles causas:\n"+
			"  • El servidor no está ejecutándose\n"+
			"  • La dirección IP/puerto es incorrecta\n"+
			"  • Hay un firewall bloqueando la conexión\n"+
			"  • El servidor no es accesible desde esta red\n"+
			"\nDetalles técnicos: %v", serverAddr, err)
	}
	fmt.Println("✓")
	scanner := bufio.NewScanner(os.Stdin)

	// Mostrar banner de bienvenida
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║         Calculadora gRPC Interactiva                      ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Printf("Conectado a: %s\n", serverAddr)
	fmt.Println()
	fmt.Println("Operadores soportados: +, -, *, /, ^")
	fmt.Println("Formato: numero operador numero [operador numero]*")
	fmt.Println("Ejemplo: 3 + 4 * 2")
	fmt.Println("         2 ^ 3 * 4 + 1")
	fmt.Println()
	fmt.Println("Comandos especiales:")
	fmt.Println("  'ejemplos' - Ejecutar ejemplos predefinidos")
	fmt.Println("  'ayuda'    - Mostrar esta ayuda")
	fmt.Println("  'salir'    - Terminar el programa")
	fmt.Println()
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Println()

	for {
		fmt.Print("Expresión> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())

		// Manejar comandos especiales
		switch strings.ToLower(input) {
		case "salir", "exit", "quit":
			fmt.Println("\n¡Hasta luego! ")
			return
		case "ayuda", "help":
			showHelp()
			continue
		case "ejemplos", "examples":
			runExamples(client)
			continue
		case "":
			continue
		}

		// Parsear y evaluar la expresión
		expr, err := parseExpression(input)
		if err != nil {
			fmt.Printf("[!] Error al parsear: %v\n\n", err)
			continue
		}

		// Evaluar en el servidor
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		result, err := client.Evaluate(ctx, expr)
		cancel()

		if err != nil {
			// Distinguir entre diferentes tipos de errores
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Printf("[!] Error: Tiempo de espera agotado. El servidor no respondió a tiempo.\n")
				fmt.Printf("    Verifica que el servidor esté funcionando correctamente.\n\n")
			} else {
				fmt.Printf("[!] Error de comunicación con el servidor:\n")
				fmt.Printf("    %v\n", err)
				fmt.Printf("    El servidor puede estar caído o inaccesible.\n\n")
			}
		} else {
			if result.Success {
				fmt.Printf("[+] Resultado: %g\n\n", result.Value)
			} else {
				fmt.Printf("[!] Error de cálculo: %s\n\n", result.Error)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error leyendo entrada: %v", err)
	}
}

// parseExpression convierte una cadena de entrada en un mensaje Expression
func parseExpression(input string) (*pb.Expression, error) {
	tokens := strings.Fields(input)

	if len(tokens) == 0 {
		return nil, fmt.Errorf("expresión vacía")
	}

	if len(tokens)%2 == 0 {
		return nil, fmt.Errorf("expresión incompleta (falta operando o operador)")
	}

	// Parsear el primer número
	firstNum, err := strconv.ParseFloat(tokens[0], 64)
	if err != nil {
		return nil, fmt.Errorf("primer número inválido '%s'", tokens[0])
	}

	expr := &pb.Expression{
		FirstNumber: firstNum,
		Operations:  []*pb.Operation{},
	}

	// Parsear el resto de operaciones (operador + número)
	for i := 1; i < len(tokens); i += 2 {
		if i+1 >= len(tokens) {
			return nil, fmt.Errorf("operación incompleta después de '%s'", tokens[i])
		}

		operator := tokens[i]

		// Validar operador
		validOperators := []string{"+", "-", "*", "/", "^"}
		isValid := false
		for _, valid := range validOperators {
			if operator == valid {
				isValid = true
				break
			}
		}
		if !isValid {
			return nil, fmt.Errorf("operador inválido '%s' (use: +, -, *, /, ^)", operator)
		}

		num, err := strconv.ParseFloat(tokens[i+1], 64)
		if err != nil {
			return nil, fmt.Errorf("número inválido '%s'", tokens[i+1])
		}

		expr.Operations = append(expr.Operations, &pb.Operation{
			Operator: operator,
			Number:   num,
		})
	}

	return expr, nil
}

// showHelp muestra la ayuda del programa
func showHelp() {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        AYUDA                              ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Operadores soportados:")
	fmt.Println("  +  Suma")
	fmt.Println("  -  Resta")
	fmt.Println("  *  Multiplicación")
	fmt.Println("  /  División")
	fmt.Println("  ^  Potencia (exponenciación)")
	fmt.Println()
	fmt.Println("Precedencia de operadores (de mayor a menor):")
	fmt.Println("  1. ^ (potencia)")
	fmt.Println("  2. * / (multiplicación y división)")
	fmt.Println("  3. + - (suma y resta)")
	fmt.Println()
	fmt.Println("Ejemplos de uso:")
	fmt.Println("  3 + 4 * 2          →  11   (primero 4*2, luego 3+8)")
	fmt.Println("  2 ^ 3 * 4 + 1      →  33   (2^3=8, 8*4=32, 32+1=33)")
	fmt.Println("  10 / 2 + 3         →  8    (10/2=5, 5+3=8)")
	fmt.Println("  5 - 3 + 2          →  4    (izquierda a derecha)")
	fmt.Println()
	fmt.Println("Comandos:")
	fmt.Println("  ejemplos  - Ejecutar ejemplos predefinidos")
	fmt.Println("  ayuda     - Mostrar esta ayuda")
	fmt.Println("  salir     - Salir del programa")
	fmt.Println()
}

// runExamples ejecuta los ejemplos predefinidos
func runExamples(client pb.CalculatorClient) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  EJEMPLOS PREDEFINIDOS                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	examples := []struct {
		description string
		expression  *pb.Expression
		input       string
	}{
		{
			description: "Precedencia de operadores: 3 + 4 - 2 * 1",
			input:       "3 + 4 - 2 * 1",
			expression: &pb.Expression{
				FirstNumber: 3,
				Operations: []*pb.Operation{
					{Operator: "+", Number: 4},
					{Operator: "-", Number: 2},
					{Operator: "*", Number: 1},
				},
			},
		},
		{
			description: "Exponenciación: 2 ^ 3 * 4 + 1",
			input:       "2 ^ 3 * 4 + 1",
			expression: &pb.Expression{
				FirstNumber: 2,
				Operations: []*pb.Operation{
					{Operator: "^", Number: 3},
					{Operator: "*", Number: 4},
					{Operator: "+", Number: 1},
				},
			},
		},
		{
			description: "División: 10 / 2 + 3",
			input:       "10 / 2 + 3",
			expression: &pb.Expression{
				FirstNumber: 10,
				Operations: []*pb.Operation{
					{Operator: "/", Number: 2},
					{Operator: "+", Number: 3},
				},
			},
		},
		{
			description: "Solo un número: 42",
			input:       "42",
			expression: &pb.Expression{
				FirstNumber: 42,
				Operations:  []*pb.Operation{},
			},
		},
		{
			description: "Expresión inválida (operador vacío)",
			input:       "3 + 4 [operador_vacío] 2",
			expression: &pb.Expression{
				FirstNumber: 3,
				Operations: []*pb.Operation{
					{Operator: "+", Number: 4},
					{Operator: "", Number: 2},
				},
			},
		},
	}

	for i, example := range examples {
		fmt.Printf("[Ejemplo %d] %s\n", i+1, example.description)
		fmt.Printf("  Entrada: %s\n", example.input)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		result, err := client.Evaluate(ctx, example.expression)
		cancel()

		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Printf("  [!] Error: Tiempo de espera agotado\n")
			} else {
				fmt.Printf("  [!] Error de comunicación: %v\n", err)
			}
		} else {
			if result.Success {
				fmt.Printf("  [+] Resultado: %g\n", result.Value)
			} else {
				fmt.Printf("  [!] Error de cálculo: %s\n", result.Error)
			}
		}
		fmt.Println()
	}

	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Println()
}

