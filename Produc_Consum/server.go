package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"strconv"

	pb "calculator/proto"
	"google.golang.org/grpc"
)

type calculatorServer struct {
	pb.UnimplementedCalculatorServer
}

// Evaluate es la única función pública del servicio
func (s *calculatorServer) Evaluate(ctx context.Context, expr *pb.Expression) (*pb.Result, error) {
	// Validar que tengamos al menos el primer número
	if expr == nil {
		return &pb.Result{
			Success: false,
			Error:   "Expresión vacía",
		}, nil
	}

	// Si no hay operaciones, devolver solo el primer número
	if len(expr.Operations) == 0 {
		return &pb.Result{
			Success: true,
			Value:   expr.FirstNumber,
		}, nil
	}

	// Validar que cada operación esté completa
	for i, op := range expr.Operations {
		if op.Operator == "" {
			return &pb.Result{
				Success: false,
				Error:   fmt.Sprintf("Operación %d: operador vacío", i+1),
			}, nil
		}
		if !isValidOperator(op.Operator) {
			return &pb.Result{
				Success: false,
				Error:   fmt.Sprintf("Operación %d: operador inválido '%s'", i+1, op.Operator),
			}, nil
		}
	}

	// Construir la expresión completa
	tokens := []string{fmt.Sprintf("%g", expr.FirstNumber)}
	for _, op := range expr.Operations {
		tokens = append(tokens, op.Operator, fmt.Sprintf("%g", op.Number))
	}

	// Evaluar la expresión respetando precedencia
	result, err := evaluateExpression(tokens)
	if err != nil {
		return &pb.Result{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.Result{
		Success: true,
		Value:   result,
	}, nil
}

// Función privada para validar operadores
func isValidOperator(op string) bool {
	switch op {
	case "+", "-", "*", "/", "^":
		return true
	default:
		return false
	}
}

// Función privada para evaluar la expresión respetando precedencia de operadores
func evaluateExpression(tokens []string) (float64, error) {
	if len(tokens) == 0 {
		return 0, fmt.Errorf("expresión vacía")
	}

	// Primero manejamos exponenciación (mayor precedencia)
	tokens = evaluateOperatorsUntilDone(tokens, []string{"^"})

	// Luego multiplicación y división
	tokens = evaluateOperatorsUntilDone(tokens, []string{"*", "/"})

	// Finalmente suma y resta
	tokens = evaluateOperatorsUntilDone(tokens, []string{"+", "-"})

	if len(tokens) != 1 {
		return 0, fmt.Errorf("error al evaluar la expresión")
	}

	return strconv.ParseFloat(tokens[0], 64)
}

// Función auxiliar que repite evaluateOperators hasta que no haya más cambios
func evaluateOperatorsUntilDone(tokens []string, operators []string) []string {
	for {
		newTokens := evaluateOperators(tokens, operators)
		// Si no hubo cambios, terminamos
		if len(newTokens) == len(tokens) {
			return newTokens
		}
		tokens = newTokens
	}
}

// Función privada para evaluar operadores específicos de izquierda a derecha
func evaluateOperators(tokens []string, operators []string) []string {
	result := []string{}
	i := 0
	
	for i < len(tokens) {
		if i+2 < len(tokens) && contains(operators, tokens[i+1]) {
			left, err := strconv.ParseFloat(tokens[i], 64)
			if err != nil {
				return tokens
			}
			right, err := strconv.ParseFloat(tokens[i+2], 64)
			if err != nil {
				return tokens
			}
			
			value := applyOperation(left, tokens[i+1], right)
			result = append(result, fmt.Sprintf("%g", value))
			i += 3
		} else {
			result = append(result, tokens[i])
			i++
		}
	}
	
	return result
}

// Función privada para aplicar una operación
func applyOperation(left float64, op string, right float64) float64 {
	switch op {
	case "+":
		return add(left, right)
	case "-":
		return subtract(left, right)
	case "*":
		return multiply(left, right)
	case "/":
		return divide(left, right)
	case "^":
		return power(left, right)
	default:
		return 0
	}
}

// Funciones privadas para cada operación
func add(a, b float64) float64 {
	return a + b
}

func subtract(a, b float64) float64 {
	return a - b
}

func multiply(a, b float64) float64 {
	return a * b
}

func divide(a, b float64) float64 {
	if b == 0 {
		return math.Inf(1) // División por cero
	}
	return a / b
}

func power(a, b float64) float64 {
	return math.Pow(a, b)
}

// Función auxiliar
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Error al escuchar: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterCalculatorServer(s, &calculatorServer{})

	log.Println("Servidor gRPC iniciado en puerto 50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Error al servir: %v", err)
	}
}
