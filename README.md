# Wega Catalog API

API Go para consulta do catalogo Wega Motors. Microservico preparado para integracao com N8N/Groq para automacao de orcamentos via WhatsApp.

> **Documentacao completa:** [docs/API.md](docs/API.md)

## Stack

- **Go 1.22** (stdlib + chi router)
- **PostgreSQL 17** (pgx v5)
- **Docker** + Coolify + Traefik

## Quick Start

```bash
# Clonar
git clone https://github.com/icl00ud/wega-catalog-api.git
cd wega-catalog-api

# Configurar
cp .env.example .env
# Editar .env com suas credenciais

# Rodar
go run ./cmd/server

# Ou com Docker
docker-compose up -d
```

## Endpoints

| Metodo | Endpoint | Descricao |
|--------|----------|-----------|
| GET | `/health` | Health check |
| GET | `/api/v1/fabricantes` | Listar marcas |
| GET | `/api/v1/tipos-filtro` | Tipos de filtro |
| POST | `/api/v1/filtros/buscar` | **Buscar filtros por veiculo** |
| GET | `/api/v1/filtros/aplicacao/{id}` | Filtros por aplicacao |
| GET | `/api/v1/referencia-cruzada?codigo=XX` | Conversao concorrente → Wega |

## Exemplo de Uso

```bash
# Buscar filtros para Gol 2020 1.0
curl -X POST http://localhost:8080/api/v1/filtros/buscar \
  -H "Content-Type: application/json" \
  -d '{"marca": "Volkswagen", "modelo": "Gol", "ano": "2020", "motor": "1.0"}'
```

**Resposta:**
```json
{
  "status": "completo",
  "veiculo": {
    "marca": "Volkswagen",
    "modelo": "Gol",
    "descricao_completa": "Gol - 1.0 3 Cil 12V - 84 cv - Total Flex..."
  },
  "filtros": [
    {"codigo_wega": "WO780", "tipo": "Filtro do Oleo"},
    {"codigo_wega": "WAP0080", "tipo": "Filtro do Ar"}
  ],
  "total_filtros": 4
}
```

## Fluxo de Integracao

```
Cliente WhatsApp
       |
       v
   N8N Webhook --> recebe "quero orcamento Gol 2020"
       |
       v
   Groq LLM --> extrai {marca, modelo, ano, motor}
       |
       v
   Wega API --> POST /api/v1/filtros/buscar
       |
       v
   Cliente WhatsApp <-- lista de filtros
```

## Estrutura do Projeto

```
wega-catalog-api/
├── cmd/server/main.go           # Entry point
├── internal/
│   ├── config/                  # Configuracoes
│   ├── database/                # Pool PostgreSQL
│   ├── handler/                 # HTTP handlers
│   ├── model/                   # Structs
│   ├── repository/              # Queries SQL
│   └── service/                 # Logica de negocio
├── docs/
│   └── API.md                   # Documentacao completa
├── Dockerfile
├── docker-compose.yaml
└── .env.example
```

## Relacionados

- [vm-oracle](https://github.com/icl00ud/vm-oracle) - Configuracoes da VM e scripts de migracao

## Licenca

MIT
