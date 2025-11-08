package main

/*
#cgo CFLAGS: -I/usr/lib/x86_64-linux-gnu/openmpi/include
#cgo LDFLAGS: -L/usr/lib/x86_64-linux-gnu/openmpi/lib -lmpi
#include <mpi.h>
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"math/rand"
	"time"
	"unsafe"
)

const (
	TOTAL_NUMBERS = 1000000
	MIN_VALUE     = 1
	MAX_VALUE     = 1000
)

func main() {
	// Initialize MPI
	C.MPI_Init(nil, nil)
	defer C.MPI_Finalize()

	var rank, size C.int
	C.MPI_Comm_rank(C.MPI_COMM_WORLD, &rank)
	C.MPI_Comm_size(C.MPI_COMM_WORLD, &size)

	var startTime time.Time
	var data []C.int
	var localSum int64
	var globalSum int64

	// Calculate how many numbers each process will receive
	localSize := TOTAL_NUMBERS / int(size)
	localData := make([]C.int, localSize)

	if rank == 0 {
		// Process 0: Generate random numbers
		startTime = time.Now()

		data = make([]C.int, TOTAL_NUMBERS)
		rand.Seed(time.Now().UnixNano())

		for i := 0; i < TOTAL_NUMBERS; i++ {
			data[i] = C.int(rand.Intn(MAX_VALUE-MIN_VALUE+1) + MIN_VALUE)
		}

		fmt.Printf("Process 0: Generated %d random numbers\n", TOTAL_NUMBERS)
		fmt.Printf("Process 0: Distributing to %d processes (%d numbers each)\n", size, localSize)
	}

	// Scatter: Distribute data from process 0 to all processes
	var sendPtr, recvPtr unsafe.Pointer
	if rank == 0 {
		sendPtr = unsafe.Pointer(&data[0])
	}
	recvPtr = unsafe.Pointer(&localData[0])

	C.MPI_Scatter(
		sendPtr,
		C.int(localSize),
		C.MPI_INT,
		recvPtr,
		C.int(localSize),
		C.MPI_INT,
		0,
		C.MPI_COMM_WORLD,
	)

	// Each process calculates the sum of its portion
	localSum = 0
	for _, value := range localData {
		localSum += int64(value)
	}

	if rank != 0 {
		fmt.Printf("Process %d: Received elements [%d-%d], local sum = %d\n",
			rank, int(rank)*localSize, (int(rank)+1)*localSize-1, localSum)
	} else {
		fmt.Printf("Process 0: Processing elements [0-%d], local sum = %d\n",
			localSize-1, localSum)
	}

	// Gather: Collect all local sums to process 0 using Reduce
	var cLocalSum, cGlobalSum C.longlong
	cLocalSum = C.longlong(localSum)

	C.MPI_Reduce(
		unsafe.Pointer(&cLocalSum),
		unsafe.Pointer(&cGlobalSum),
		1,
		C.MPI_LONG_LONG,
		C.MPI_SUM,
		0,
		C.MPI_COMM_WORLD,
	)

	globalSum = int64(cGlobalSum)

	// Process 0 displays the final result
	if rank == 0 {
		elapsed := time.Since(startTime)

		fmt.Printf("\n=== RESULTS ===\n")
		fmt.Printf("Total sum: %d\n", globalSum)
		fmt.Printf("Number of processes: %d\n", size)
		fmt.Printf("Execution time: %v\n", elapsed)
		fmt.Printf("Execution time (ms): %.2f\n", float64(elapsed.Nanoseconds())/1e6)
	}
}
