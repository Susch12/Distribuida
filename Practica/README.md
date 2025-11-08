# Parallel Sum with MPI in Go

This project implements a parallel sum using MPI (Message Passing Interface) scatter and gather operations in Go.

## Description

The program distributes 1 million random numbers (between 1 and 1000) across multiple processes:
- **Process 0**: Generates the random numbers and distributes them
- **Worker processes**: Each receives a portion of the data, calculates the sum
- **Process 0**: Gathers all partial sums and calculates the total

## Requirements

- Go 1.21 or higher
- OpenMPI or MPICH installed on your system
- MPI Go bindings

### Installing MPI on Ubuntu/Debian

```bash
sudo apt-get update
sudo apt-get install openmpi-bin openmpi-dev libopenmpi-dev
```

### Installing Go dependencies

```bash
go mod download
```

## Building

```bash
go build -o parallel_sum parallel_sum.go
```

## Running

### Single run with specific number of processes

```bash
mpirun -np 10 ./parallel_sum
```

### Running benchmarks

To run benchmarks with 10, 20, 40, 50, and 100 processes:

```bash
./benchmark.sh
```

This will generate a comparative table showing execution times for different process counts.

## Expected Output

```
Process 0: Generated 1000000 random numbers
Process 0: Distributing to 10 processes (100000 numbers each)
Process 1: Received elements [100000-199999], local sum = 50012345
Process 2: Received elements [200000-299999], local sum = 50023456
...

=== RESULTS ===
Total sum: 500123456
Number of processes: 10
Execution time: 123.45ms
Execution time (ms): 123.45
```

## Performance Analysis

The benchmark results will help answer: **Does more processes always mean better performance?**

Generally, the answer is **NO** due to:

1. **Communication Overhead**: Scatter and gather operations have a cost
2. **Process Creation**: Starting more processes takes time
3. **Synchronization Costs**: Coordinating many processes adds overhead
4. **Diminishing Returns**: Less work per process means overhead dominates
5. **Hardware Limits**: Can't efficiently use more processes than CPU cores

## File Structure

- `parallel_sum.go` - Main MPI program
- `benchmark.sh` - Automated benchmark script
- `go.mod` - Go module dependencies
- `README.md` - This file
