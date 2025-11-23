#!/bin/bash

# Script para otimizar o SQLite para leitura concorrente

echo "🔧 Otimizando database.db para leitura concorrente..."

# Verificar se database.db existe
if [ ! -f "database.db" ]; then
    echo "❌ Erro: database.db não encontrado!"
    echo "Execute primeiro: go run cmd/seeder/main.go"
    exit 1
fi

# Habilitar WAL mode (Write-Ahead Logging)
# Permite múltiplos leitores simultâneos
sqlite3 database.db "PRAGMA journal_mode=WAL;"

# Otimizações adicionais
sqlite3 database.db "PRAGMA synchronous=NORMAL;"
sqlite3 database.db "PRAGMA cache_size=10000;"
sqlite3 database.db "PRAGMA temp_store=MEMORY;"

# Verificar
echo ""
echo "✅ Configurações aplicadas:"
sqlite3 database.db "PRAGMA journal_mode;"
sqlite3 database.db "PRAGMA synchronous;"

echo ""
echo "🎉 database.db otimizado!"
echo ""
echo "Agora você pode rodar:"
echo "  docker-compose -f docker-compose.mom.yml up --build"