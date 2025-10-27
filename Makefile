# Makefile para PolarysDB

.PHONY: proto clean test bench install-tools

# Generar código Protocol Buffers
proto:
	@echo "Generating Protocol Buffers code..."
	protoc --go_out=. --go_opt=paths=source_relative \
		modules/wal/proto/wal.proto
	@echo "✅ Protocol Buffers code generated successfully"

# Instalar herramientas necesarias
install-tools:
	@echo "Installing development tools..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@echo "✅ Tools installed successfully"
	@echo "⚠️  Make sure 'protoc' is installed on your system:"
	@echo "    - macOS: brew install protobuf"
	@echo "    - Linux: apt-get install -y protobuf-compiler"
	@echo "    - Windows: choco install protoc"

# Ejecutar tests
test:
	@echo "Running tests..."
	go test -v ./...

# Ejecutar benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./benchmarks/...

# Limpiar archivos generados
clean:
	@echo "Cleaning generated files..."
	find . -name "*.pb.go" -type f -delete
	rm -rf ./data* ./bench* ./stress* ./backups*
	@echo "✅ Clean completed"

# Compilar el proyecto
build:
	@echo "Building PolarysDB..."
	go build -o bin/polarysdb ./cmd/polarysdb
	@echo "✅ Build completed"

# Ejecutar linter
lint:
	@echo "Running linter..."
	golangci-lint run
	@echo "✅ Lint completed"

# Formatear código
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "✅ Format completed"

# Verificar dependencias
deps:
	@echo "Verifying dependencies..."
	go mod tidy
	go mod verify
	@echo "✅ Dependencies verified"

# Target por defecto
all: install-tools proto build test

# Ayuda
help:
	@echo "Available targets:"
	@echo "  proto         - Generate Protocol Buffers code"
	@echo "  install-tools - Install required development tools"
	@echo "  test          - Run all tests"
	@echo "  bench         - Run benchmarks"
	@echo "  build         - Build the project"
	@echo "  clean         - Remove generated files"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo "  deps          - Verify dependencies"
	@echo "  all           - Run install-tools, proto, build, and test"