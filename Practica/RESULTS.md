# Benchmark Results - Parallel Sum with MPI

## Test Configuration
- **Total numbers**: 1,000,000
- **Number range**: 1 to 1000
- **MPI Implementation**: OpenMPI 4.1.6
- **Language**: Go 1.25.4

## Performance Results

| Number of Processes | Execution Time (ms) | Speedup vs 10 | Efficiency |
|--------------------:|--------------------:|--------------:|-----------:|
| 10                  | 117.47              | 1.00x         | 100%       |
| 20                  | 88.79               | 1.32x         | 66%        |
| 40                  | 108.63              | 1.08x         | 27%        |
| 50                  | 357.14              | 0.33x         | 7%         |
| 100                 | 349.91              | 0.34x         | 3%         |

## Analysis: Does More Processes Always Mean Better Performance?

**Answer: NO**

The results clearly demonstrate that **increasing the number of processes does NOT always improve performance**.

### Key Observations

1. **Best Performance**: 20 processes achieved the best time (88.79ms), which is 1.32x faster than 10 processes

2. **Performance Degradation**: After 20 processes, performance significantly degrades:
   - 40 processes: slightly slower than 10 processes
   - 50 processes: 3x SLOWER than 10 processes
   - 100 processes: 3x SLOWER than 10 processes

### Why This Happens

1. **Communication Overhead**
   - Scatter operation cost increases with more processes
   - Reduce/gather operation cost increases with more processes
   - Network bandwidth becomes a bottleneck

2. **Process Management Overhead**
   - Creating and initializing 100 processes takes significant time
   - Context switching between processes adds overhead
   - Synchronization barriers slow down execution

3. **Diminishing Work Per Process**
   - With 100 processes: each process only handles 10,000 numbers
   - The computation time becomes smaller than communication time
   - Overhead dominates actual useful work

4. **Hardware Limitations**
   - System likely has fewer CPU cores than processes (oversubscribing)
   - Multiple processes compete for the same CPU cores
   - Memory bandwidth contention increases

### Optimal Point

For this specific problem on this hardware:
- **Optimal**: ~20 processes
- **Acceptable**: 10-40 processes
- **Inefficient**: 50+ processes

### Conclusion

**More processes ≠ Better performance**

There is an optimal number of processes that balances:
- Parallel computation benefits
- Communication overhead costs
- Hardware resource availability

Beyond this optimal point, adding more processes actually **hurts** performance due to excessive overhead.

## Verification

The sums calculated are consistent across all runs:
- All totals are approximately 500 million
- Expected average: 500 (midpoint) × 1,000,000 = 500,000,000
- Results confirm correct implementation
