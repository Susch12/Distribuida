# Installation Guide

## Prerequisites

This project requires MPI (Message Passing Interface) and Go with MPI bindings.

## Step 1: Install OpenMPI

### Ubuntu/Debian
```bash
sudo apt-get update
sudo apt-get install -y openmpi-bin openmpi-dev libopenmpi-dev
```

### Verify installation
```bash
mpirun --version
```

You should see output like:
```
mpirun (Open MPI) 4.x.x
```

## Step 2: Install Go (if not installed)

Go is already installed on your system (version 1.25.4).

## Step 3: Install Go MPI Bindings

The project uses the `github.com/marcin-krolik/mpi` package. Install dependencies:

```bash
go mod download
```

## Step 4: Build the Project

```bash
make build
```

Or manually:
```bash
go build -o parallel_sum parallel_sum.go
```

## Step 5: Test the Installation

Run a quick test with 10 processes:

```bash
make test
```

Or manually:
```bash
mpirun -np 10 ./parallel_sum
```

## Troubleshooting

### Error: "mpirun: command not found"
- MPI is not installed or not in your PATH
- Follow Step 1 to install OpenMPI

### Error: "cannot find package github.com/marcin-krolik/mpi"
- Run `go mod download` to fetch dependencies
- Ensure you have an active internet connection

### Error: Building fails with CGO errors
- Install MPI development libraries:
  ```bash
  sudo apt-get install libopenmpi-dev
  ```

### Error: "Failed to initialize MPI"
- Ensure OpenMPI is properly installed
- Try running with explicit host: `mpirun -np 10 -host localhost ./parallel_sum`

## Next Steps

Once installation is complete, refer to `README.md` for usage instructions.
