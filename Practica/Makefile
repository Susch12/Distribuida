.PHONY: build run clean benchmark test help

# Default number of processes
NPROCS ?= 10

help:
	@echo "Available commands:"
	@echo "  make build      - Build the parallel sum program"
	@echo "  make run        - Run with default 10 processes"
	@echo "  make run20      - Run with 20 processes"
	@echo "  make run50      - Run with 50 processes"
	@echo "  make benchmark  - Run full benchmark suite"
	@echo "  make test       - Run a quick test with 10 processes"
	@echo "  make clean      - Remove built binary"

build:
	@echo "Building parallel_sum..."
	go build -o parallel_sum parallel_sum.go

run: build
	@echo "Running with $(NPROCS) processes..."
	mpirun -np $(NPROCS) ./parallel_sum

run10: build
	mpirun -np 10 ./parallel_sum

run20: build
	mpirun -np 20 ./parallel_sum

run40: build
	mpirun -np 40 ./parallel_sum

run50: build
	mpirun -np 50 ./parallel_sum

run100: build
	mpirun -np 100 ./parallel_sum

benchmark: build
	@echo "Running benchmark suite..."
	./benchmark.sh

test: run10

clean:
	rm -f parallel_sum
	@echo "Cleaned build artifacts"
