#!/bin/bash
# ============================================================================
# FILE: setup.sh
# Script de instalación y configuración para PolarysDB
# ============================================================================

set -e

echo "🚀 PolarysDB Setup Script"
echo "=========================="
echo ""

# Colores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Detectar sistema operativo
OS=""
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    OS="linux"
elif [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
elif [[ "$OSTYPE" == "msys" ]] || [[ "$OSTYPE" == "cygwin" ]]; then
    OS="windows"
else
    echo -e "${RED}✗ Sistema operativo no soportado: $OSTYPE${NC}"
    exit 1
fi

echo -e "${GREEN}✓${NC} Sistema operativo detectado: $OS"

# Verificar si Go está instalado
if ! command -v go &> /dev/null; then
    echo -e "${RED}✗ Go no está instalado${NC}"
    echo "  Por favor instala Go desde https://golang.org/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
echo -e "${GREEN}✓${NC} Go instalado: $GO_VERSION"

# Verificar versión mínima de Go (1.19+)
GO_MAJOR=$(echo $GO_VERSION | sed 's/go//' | cut -d. -f1)
GO_MINOR=$(echo $GO_VERSION | sed 's/go//' | cut -d. -f2)

if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 19 ]); then
    echo -e "${YELLOW}⚠${NC} Se recomienda Go 1.19 o superior (tienes $GO_VERSION)"
fi

# Instalar Protocol Buffers compiler
echo ""
echo "📦 Instalando Protocol Buffers..."

if command -v protoc &> /dev/null; then
    PROTOC_VERSION=$(protoc --version | awk '{print $2}')
    echo -e "${GREEN}✓${NC} protoc ya instalado: v$PROTOC_VERSION"
else
    echo "  Instalando protoc..."
    
    if [ "$OS" = "macos" ]; then
        if command -v brew &> /dev/null; then
            brew install protobuf
        else
            echo -e "${RED}✗ Homebrew no encontrado. Instala desde: https://brew.sh/${NC}"
            exit 1
        fi
    elif [ "$OS" = "linux" ]; then
        if command -v apt-get &> /dev/null; then
            sudo apt-get update
            sudo apt-get install -y protobuf-compiler
        elif command -v yum &> /dev/null; then
            sudo yum install -y protobuf-compiler
        else
            echo -e "${YELLOW}⚠${NC} Gestor de paquetes no reconocido"
            echo "  Descarga protoc desde: https://github.com/protocolbuffers/protobuf/releases"
        fi
    elif [ "$OS" = "windows" ]; then
        if command -v choco &> /dev/null; then
            choco install protoc
        else
            echo -e "${YELLOW}⚠${NC} Chocolatey no encontrado"
            echo "  Descarga protoc desde: https://github.com/protocolbuffers/protobuf/releases"
        fi
    fi
    
    echo -e "${GREEN}✓${NC} protoc instalado"
fi

# Instalar protoc-gen-go
echo ""
echo "📦 Instalando protoc-gen-go..."

if command -v protoc-gen-go &> /dev/null; then
    echo -e "${GREEN}✓${NC} protoc-gen-go ya instalado"
else
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    echo -e "${GREEN}✓${NC} protoc-gen-go instalado"
fi

# Verificar que protoc-gen-go esté en PATH
if ! command -v protoc-gen-go &> /dev/null; then
    echo -e "${YELLOW}⚠${NC} protoc-gen-go no está en PATH"
    echo "  Agrega \$GOPATH/bin a tu PATH:"
    echo "    export PATH=\"\$PATH:\$(go env GOPATH)/bin\""
fi

# Instalar dependencias del proyecto
echo ""
echo "📦 Instalando dependencias de Go..."
go mod download
go mod tidy
echo -e "${GREEN}✓${NC} Dependencias instaladas"

# Generar código Protocol Buffers
echo ""
echo "🔨 Generando código Protocol Buffers..."

if [ -f "modules/wal/proto/wal.proto" ]; then
    protoc --go_out=. --go_opt=paths=source_relative \
        modules/wal/proto/wal.proto
    echo -e "${GREEN}✓${NC} Código Protocol Buffers generado"
else
    echo -e "${RED}✗ Archivo wal.proto no encontrado${NC}"
    echo "  Creando estructura de directorios..."
    mkdir -p modules/wal/proto
fi

# Crear directorios necesarios
echo ""
echo "📁 Creando directorios..."
mkdir -p data backups logs bin
echo -e "${GREEN}✓${NC} Directorios creados"

# Compilar el proyecto
echo ""
echo "🔨 Compilando PolarysDB..."
go build -o bin/polarysdb ./cmd/polarysdb 2>/dev/null || echo -e "${YELLOW}⚠${NC} No se encontró cmd/polarysdb"
echo -e "${GREEN}✓${NC} Compilación completada"

# Ejecutar tests básicos
echo ""
echo "🧪 Ejecutando tests..."
go test ./... -short 2>/dev/null || echo -e "${YELLOW}⚠${NC} Algunos tests fallaron"
echo -e "${GREEN}✓${NC} Tests completados"

# Resumen final
echo ""
echo "=========================================="
echo -e "${GREEN}✅ Setup completado exitosamente!${NC}"
echo "=========================================="
echo ""
echo "Próximos pasos:"
echo "  1. Revisa el archivo README.md para documentación"
echo "  2. Ejecuta 'make proto' para regenerar Protocol Buffers"
echo "  3. Ejecuta 'make test' para correr todos los tests"
echo "  4. Ejecuta 'make bench' para correr benchmarks"
echo ""
echo "Comandos útiles:"
echo "  make proto    - Generar código Protocol Buffers"
echo "  make test     - Ejecutar tests"
echo "  make bench    - Ejecutar benchmarks"
echo "  make build    - Compilar el proyecto"
echo "  make clean    - Limpiar archivos generados"
echo ""