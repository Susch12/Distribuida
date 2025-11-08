#!/bin/bash

# Script to benchmark the parallel sum with different numbers of processes

echo "Building the Go program..."
go build -o parallel_sum parallel_sum.go

if [ $? -ne 0 ]; then
    echo "Build failed!"
    exit 1
fi

echo "Running benchmarks..."
echo ""

# Array to store results
declare -a PROCESSES=(10 20 40 50 100)
declare -a TIMES=()

# Run benchmark for each process count
for NPROCS in "${PROCESSES[@]}"; do
    echo "Running with $NPROCS processes..."

    # Run the program and capture the execution time
    OUTPUT=$(mpirun --oversubscribe -np $NPROCS ./parallel_sum 2>&1)

    # Extract the execution time in milliseconds
    TIME=$(echo "$OUTPUT" | grep "Execution time (ms):" | awk '{print $4}')

    TIMES+=("$TIME")

    echo "$OUTPUT"
    echo "----------------------------------------"
    echo ""
done

# Generate comparative table
echo ""
echo "=========================================="
echo "       COMPARATIVE RESULTS TABLE"
echo "=========================================="
echo ""
printf "%-20s | %-20s\n" "Number of Processes" "Execution Time (ms)"
echo "----------------------------------------"

for i in "${!PROCESSES[@]}"; do
    printf "%-20d | %-20s\n" "${PROCESSES[$i]}" "${TIMES[$i]}"
done

echo "=========================================="
echo ""
echo "Analysis: Does more processes always mean better performance?"
echo ""
echo "The results may show that increasing the number of processes"
echo "doesn't always improve performance due to:"
echo "  1. Communication overhead (scatter/gather operations)"
echo "  2. Process creation and synchronization costs"
echo "  3. Diminishing returns as work per process decreases"
echo "  4. Hardware limitations (CPU cores available)"
