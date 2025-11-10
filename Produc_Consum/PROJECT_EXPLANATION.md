# gRPC Calculator Service - Team Presentation Guide

## 1. Introduction (2-3 minutes)

### What is this project?
A **distributed calculator service** built with gRPC that evaluates mathematical expressions with proper operator precedence.

### Key Technologies:
- **gRPC**: High-performance RPC framework by Google
- **Protocol Buffers**: Language-agnostic data serialization
- **Go**: Implementation language
- **Client-Server Architecture**: Distributed systems pattern

---

## 2. Architecture Overview (5 minutes)

### High-Level Architecture

```
┌──────────┐                      ┌──────────┐
│  Client  │ ───── gRPC ─────────>│  Server  │
│          │  (TCP :50051)        │          │
│ client.go│<──── Response ───────│server.go │
└──────────┘                      └──────────┘
                                       │
                                       ▼
                               ┌──────────────┐
                               │  Expression  │
                               │   Evaluator  │
                               └──────────────┘
```

### Components:

1. **Protocol Definition** (`calculator.proto`)
   - Defines service contract
   - Message structures
   - RPC methods

2. **Server** (`server.go`)
   - Implements calculator logic
   - Handles client requests
   - Evaluates expressions with precedence

3. **Client** (`client.go`)
   - Connects to server
   - Sends test expressions
   - Displays results

4. **Generated Code** (`proto/`)
   - Auto-generated from .proto file
   - Handles serialization/deserialization
   - Provides type-safe interfaces

---

## 3. Protocol Buffers Definition (3-4 minutes)

### The Service Contract (`calculator.proto`)

```protobuf
service Calculator {
  rpc Evaluate(Expression) returns (Result);
}
```

**Why only ONE public method?**
- Simplicity and focused responsibility
- All operations go through a single endpoint
- Easier to maintain and secure

### Message Structures

```protobuf
message Expression {
  double first_number = 1;           // Starting number (e.g., 3)
  repeated Operation operations = 2;  // Chain of operations
}

message Operation {
  string operator = 1;  // +, -, *, /, ^
  double number = 2;    // Operand
}

message Result {
  bool success = 1;     // Operation successful?
  double value = 2;     // Result value
  string error = 3;     // Error message if failed
}
```

### Example Expression: `3 + 4 * 2`

```
Expression {
  first_number: 3
  operations: [
    {operator: "+", number: 4},
    {operator: "*", number: 2}
  ]
}
```

---

## 4. How It Works - Step by Step (5-7 minutes)

### Flow Diagram

```
Client sends:           Server receives:         Server processes:
  3 + 4 * 2        →    Expression object    →   1. Validate
                            ↓                     2. Parse to tokens
                        [3, +, 4, *, 2]           3. Apply precedence
                            ↓                     4. Evaluate
                        Pass 1: * and /              ↓
                        [3, +, 8]              Result: 11
                            ↓
                        Pass 2: + and -
                        [11]
```

### Server Evaluation Algorithm

**Multi-Pass Precedence Evaluation:**

1. **Pass 1**: Evaluate exponentiation (`^`)
   - `2 ^ 3 * 4` → `8 * 4`

2. **Pass 2**: Evaluate multiplication/division (`*`, `/`)
   - `8 * 4` → `32`

3. **Pass 3**: Evaluate addition/subtraction (`+`, `-`)
   - `32 + 1` → `33`

### Code Walkthrough (server.go)

```go
// 1. Validate expression
func (s *calculatorServer) Evaluate(ctx context.Context, expr *pb.Expression) (*pb.Result, error) {
    // Check for empty expression
    // Validate operators

    // 2. Convert to tokens: [3, +, 4, *, 2]
    tokens := []string{fmt.Sprintf("%g", expr.FirstNumber)}
    for _, op := range expr.Operations {
        tokens = append(tokens, op.Operator, fmt.Sprintf("%g", op.Number))
    }

    // 3. Evaluate with precedence
    result, err := evaluateExpression(tokens)

    // 4. Return result
    return &pb.Result{Success: true, Value: result}, nil
}
```

---

## 5. Live Demo Script (5-7 minutes)

### Terminal 1: Start Server

```bash
# Show the directory structure
ls -la

# Start the server
make server
# or
./bin/server

# Output: "Servidor gRPC iniciado en puerto 50051"
```

### Terminal 2: Run Client

```bash
# Run the client with test cases
make client
# or
./bin/client
```

### Expected Output:

```
=== Ejemplo 1: 3 + 4 - 2 * 1 ===
Resultado: 5

=== Ejemplo 2: 2 ^ 3 * 4 + 1 ===
Resultado: 33

=== Ejemplo 3: Expresión inválida ===
Error: Operación 2: operador vacío

=== Ejemplo 4: Solo un número (42) ===
Resultado: 42

=== Ejemplo 5: 10 / 2 + 3 ===
Resultado: 8
```

### Explain Each Result:

1. **Example 1**: `3 + 4 - 2 * 1 = 3 + 4 - 2 = 5`
   - Multiplication first: `2 * 1 = 2`
   - Then left-to-right: `3 + 4 = 7`, `7 - 2 = 5`

2. **Example 2**: `2 ^ 3 * 4 + 1 = 8 * 4 + 1 = 33`
   - Exponentiation first: `2 ^ 3 = 8`
   - Multiplication: `8 * 4 = 32`
   - Addition: `32 + 1 = 33`

3. **Example 3**: Error handling demonstration
   - Shows validation works

4. **Example 4**: Simple edge case
   - No operations needed

5. **Example 5**: Division with addition
   - Division first: `10 / 2 = 5`
   - Addition: `5 + 3 = 8`

---

## 6. Key Features & Design Decisions (3-4 minutes)

### ✅ Features Implemented

1. **Proper Operator Precedence**
   - Mathematical correctness
   - Multi-pass evaluation algorithm

2. **Comprehensive Validation**
   - Empty expressions
   - Invalid operators
   - Missing operands

3. **Error Handling**
   - Graceful error messages
   - No server crashes
   - Division by zero returns Infinity

4. **Type Safety**
   - Protocol Buffers ensure type correctness
   - Compile-time checks

### 🎯 Design Decisions

**Why gRPC over REST?**
- ✅ Better performance (binary protocol vs JSON)
- ✅ Type-safe contracts (protobuf vs OpenAPI)
- ✅ Bi-directional streaming support
- ✅ Built-in code generation
- ✅ Better suited for microservices

**Why one public method?**
- Single Responsibility Principle
- Easier to extend (add new operators without new endpoints)
- Simpler API surface
- Unified error handling

**Why Go?**
- Excellent gRPC support
- High performance
- Simple concurrency
- Great for distributed systems

---

## 7. Project Structure & Build System (3 minutes)

### Directory Layout

```
gRPC/
├── calculator.proto          # API contract
├── server.go                 # Server implementation
├── client.go                 # Client + tests
├── Makefile                  # Build automation
├── go.mod/go.sum            # Dependencies
├── proto/                    # Generated code
│   ├── calculator.pb.go
│   └── calculator_grpc.pb.go
├── scripts/                  # Helper scripts
│   ├── setup.sh             # Environment setup
│   ├── generate_proto.sh    # Code generation
│   ├── fix.sh               # Dependency fixes
│   └── quick_fix.sh         # Quick repairs
└── bin/                      # Compiled binaries
    ├── server
    └── client
```

### Build Commands

```bash
# Setup environment (first time)
make setup

# Generate protobuf code
make proto

# Build everything
make build

# Run server
make server

# Run client (in another terminal)
make client

# Clean generated files
make clean
```

---

## 8. Testing Strategy (2-3 minutes)

### Current Test Cases (in client.go)

| Test | Expression | Expected | Purpose |
|------|-----------|----------|---------|
| 1 | `3 + 4 - 2 * 1` | 5 | Precedence test |
| 2 | `2 ^ 3 * 4 + 1` | 33 | Exponentiation precedence |
| 3 | Empty operator | Error | Validation test |
| 4 | `42` | 42 | Single number edge case |
| 5 | `10 / 2 + 3` | 8 | Division precedence |

### How to Add More Tests

```go
// In client.go, add:
exprNew := &pb.Expression{
    FirstNumber: 5,
    Operations: []*pb.Operation{
        {Operator: "*", Number: 2},
        {Operator: "+", Number: 3},
    },
}
resultNew, err := client.Evaluate(ctx, exprNew)
// Expected: 5 * 2 + 3 = 13
```

---

## 9. Potential Extensions (2-3 minutes)

### Easy Additions:

1. **More Operators**
   ```protobuf
   // Add to isValidOperator() in server.go
   case "%": return modulo(a, b)      // Modulo
   case "sqrt": return math.Sqrt(a)   // Square root
   ```

2. **Parentheses Support**
   - Would require parsing algorithm change
   - Implement Shunting Yard algorithm

3. **Variable Support**
   ```protobuf
   message Expression {
       map<string, double> variables = 3;
       // e.g., {"x": 5, "y": 10}
   }
   ```

4. **Expression History**
   - Store recent calculations
   - Add new RPC: `GetHistory()`

### Advanced Features:

1. **Authentication & Authorization**
   - Add gRPC interceptors
   - JWT tokens

2. **Logging & Monitoring**
   - Request logging
   - Metrics (Prometheus)
   - Distributed tracing

3. **Load Balancing**
   - Multiple server instances
   - Client-side load balancing

4. **Streaming Support**
   ```protobuf
   rpc EvaluateStream(stream Expression) returns (stream Result);
   ```

---

## 10. Common Issues & Solutions (2 minutes)

### Problem: "protoc: command not found"

```bash
# Ubuntu/Debian
sudo apt-get install -y protobuf-compiler

# macOS
brew install protobuf
```

### Problem: "protoc-gen-go: plugin not found"

```bash
# Install plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Add to PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

### Problem: "cannot find package calculator/proto"

```bash
# Regenerate proto files
make proto

# Or use the script
./scripts/generate_proto.sh
```

### Problem: Version compatibility errors

```bash
# Run quick fix
./scripts/quick_fix.sh
```

---

## 11. Q&A Preparation

### Expected Questions:

**Q: Why not use REST?**
A: gRPC is faster (binary vs JSON), has type safety via protobuf, and better supports microservices patterns.

**Q: How does the server handle concurrent requests?**
A: gRPC's Go implementation uses goroutines automatically for each request.

**Q: What about security?**
A: Currently uses insecure credentials for simplicity. In production, we'd add TLS and authentication.

**Q: Can we add more complex math operations?**
A: Yes! Just extend the operator validation and add corresponding functions.

**Q: How would you deploy this?**
A: Containerize with Docker, deploy to Kubernetes, add load balancer, implement health checks.

**Q: What about error handling if server crashes?**
A: Client should implement retry logic with exponential backoff. We could add circuit breakers.

---

## 12. Presentation Tips

### Before You Start:
- [ ] Have two terminals ready
- [ ] Test the demo beforehand
- [ ] Have the code open in your editor
- [ ] Run `make build` to ensure everything compiles

### During Presentation:
1. Start with the big picture (architecture diagram)
2. Show the .proto file first (the contract)
3. Walk through one complete request flow
4. Do the live demo
5. Show interesting code snippets
6. Discuss design decisions
7. Open for questions

### What to Emphasize:
- **Simplicity**: One public method handles everything
- **Type Safety**: Protocol Buffers prevent errors
- **Performance**: Binary protocol is fast
- **Extensibility**: Easy to add new operators
- **Real-world applicability**: This pattern scales to complex microservices

### What to Avoid:
- Don't dive too deep into generated code
- Don't get lost in implementation details
- Keep the focus on concepts and architecture
- Save complex questions for after

---

## 13. Quick Reference

### Start Demo:
```bash
# Terminal 1
cd /home/jesus/Documents/uni/Distribuida/gRPC
make server

# Terminal 2
make client
```

### Key Files to Show:
1. `calculator.proto` - The contract
2. `server.go:20-72` - Evaluate method
3. `server.go:85-104` - Precedence algorithm
4. `client.go:27-45` - Example usage

### Key Concepts:
- **gRPC**: Remote Procedure Call framework
- **Protocol Buffers**: Interface Definition Language
- **Operator Precedence**: Mathematical correctness
- **Client-Server**: Distributed systems pattern

---

## 14. Conclusion Points

### Summary:
- Built a production-ready calculator service with gRPC
- Demonstrates distributed systems concepts
- Proper error handling and validation
- Extensible architecture for future enhancements
- Real-world applicability to microservices

### Learning Outcomes:
- Understanding gRPC and Protocol Buffers
- Client-server communication patterns
- API design principles
- Go programming for distributed systems
- Build automation with Make

### Next Steps:
- Add authentication
- Implement monitoring
- Add more operators
- Deploy to production environment
- Scale horizontally with load balancing

---

Good luck with your presentation! 🚀
