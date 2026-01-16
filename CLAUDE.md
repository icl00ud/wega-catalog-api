# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Wega Catalog API is a Go microservice for querying the Wega Motors automotive parts catalog. It's designed for integration with N8N/Groq to automate quotations via WhatsApp. The API accepts natural language queries (extracted by LLM) and returns compatible filter parts for vehicles.

**Key Integration Flow:**
```
WhatsApp Client → N8N Webhook → Groq LLM (extracts vehicle data) → Wega API → PostgreSQL → Response
```

## Development Commands

### Running the Application

```bash
# Run locally (requires PostgreSQL connection)
go run ./cmd/server

# Build binary
go build -o wega-api ./cmd/server

# Run with Docker
docker-compose up -d

# Build Docker image
docker build -t wega-catalog-api .
```

### Go Module Management

```bash
# Download dependencies
go mod download

# Tidy dependencies (remove unused)
go mod tidy

# Verify dependencies
go mod verify
```

### Testing

There are currently no tests in the codebase. When adding tests:
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/service/...
```

## Architecture Overview

### Layered Architecture Pattern

The codebase follows a clean layered architecture with clear separation of concerns:

```
cmd/server/main.go          → Entry point, dependency wiring
    ↓
internal/handler/           → HTTP handlers (chi router)
    ↓
internal/service/           → Business logic layer
    ↓
internal/repository/        → Data access layer (SQL queries)
    ↓
internal/database/          → Connection pool management
```

**Key principle:** Dependencies flow downward. Repositories don't know about services, services don't know about handlers.

### Core Service: CatalogoService

The main business logic lives in `internal/service/catalogo_service.go`. It orchestrates multi-step filter searches:

1. **Validation** - Checks for required fields (marca, modelo)
2. **Search** - Queries APLICACAO table for matching vehicles
3. **Disambiguation** - Returns "incompleto" or "multiplos" status when user input is ambiguous
4. **Filter Lookup** - Fetches compatible filters from PRODUTO_APLICACAO join table
5. **Response Assembly** - Formats data with appropriate status codes

**Response Status Flow:**
- `completo` → Filters found successfully
- `incompleto` → Missing year/motor, returns available options
- `multiplos` → Multiple distinct vehicles match, user must choose
- `nao_encontrado` → No matching vehicle in catalog

### Database Schema Relationships

PostgreSQL 17 with mixed-case table names (legacy schema):

**Core Tables:**
- `FABRICANTE` (497 rows) - Vehicle and competitor brands
  - `FlagAplicacao = 1` for vehicle manufacturers
  - `FlagAplicacao = 0` for competitor brands
- `APLICACAO` (49,034 rows) - Vehicle models/versions (e.g., "Gol 1.0 2020")
- `PRODUTO` (3,432 rows) - Wega filter parts
- `PRODUTO_APLICACAO` (51,426 rows) - N:N relationship between filters and vehicles
- `SUBGRUPOPRODUTO` (29 rows) - Filter types (Oil, Air, Fuel, etc.)
- `REFERENCIACRUZADA` (34,243 rows) - Competitor part number equivalencies

**Critical Indexes (already exist):**
```sql
idx_aplicacao_fabricante      → Fast brand lookups
idx_aplicacao_descricao        → Full-text search on Portuguese descriptions
idx_produto_aplicacao_*        → Fast join table queries
idx_referencia_pesq            → Cross-reference searches
```

### Repository Pattern Details

Repositories use pgx/v5 (not database/sql) for connection pooling:

- **Dynamic Query Building** - Repositories build WHERE clauses dynamically based on provided filters
- **ILIKE Pattern Matching** - Portuguese text search uses case-insensitive ILIKE with wildcards
- **NULL Coalescing** - Motor/year fields may be NULL, coalesced to empty strings
- **Context-Aware** - All queries accept `context.Context` for cancellation/timeouts

Example from `aplicacao_repo.go`:
```go
// Builds query with optional filters (marca, modelo, ano, motor)
// Uses positional parameters ($1, $2, ...) for SQL injection safety
```

### HTTP Layer (Chi Router)

`cmd/server/main.go` wires up the HTTP stack:

**Middleware Stack:**
1. RequestID - Generates unique IDs for tracing
2. RealIP - Extracts real client IP behind proxies
3. Logger - Structured logging of requests
4. Recoverer - Panic recovery
5. Timeout (30s) - Prevents hanging requests
6. CORS - Wide-open (* origin) for N8N integration

**Routes:**
- `/health` - Database connection check
- `/api/v1/fabricantes` - List manufacturers (query param `tipo=concorrente` for competitors)
- `/api/v1/tipos-filtro` - List filter types
- `/api/v1/filtros/buscar` - **Main endpoint** - Search filters by vehicle
- `/api/v1/filtros/aplicacao/{id}` - Get filters by application ID
- `/api/v1/referencia-cruzada?codigo=XX` - Competitor part cross-reference

### Configuration Management

Environment-based config in `internal/config/config.go`:

**Database Connection Pooling:**
- `DB_MAX_CONNS=25` - Max concurrent connections (default: 25)
- `DB_MIN_CONNS=5` - Idle connection pool size (default: 5)

**Performance Tuning:** The pool size is tuned for ~50 concurrent API requests. If deploying at scale, increase MAX_CONNS proportionally (100 RPS → 50 max conns).

### Graceful Shutdown

The server implements proper graceful shutdown:
1. SIGINT/SIGTERM signals caught
2. 10-second timeout for in-flight requests
3. Connection pool closed before exit

## Database Connection

The API connects to a PostgreSQL 17 instance running in Docker (Coolify managed).

**Default Connection (from .env.example):**
```
Host: o8cok8s4cg408cos4k0sowos
Database: wega
User: wega
Password: WegaCat_2026_Secure!
Port: 5432
SSL Mode: disable (internal Docker network)
```

**Important:** The database is pre-populated with Wega catalog data. There are no migration scripts in this repository - see [vm-oracle](https://github.com/icl00ud/vm-oracle) for schema setup.

## Deployment

### Docker Compose (Coolify)

The `docker-compose.yaml` is configured for Coolify deployment with:
- **Traefik Labels** - Auto-configured SSL via Let's Encrypt
- **Network** - Joins `coolify` external network to access PostgreSQL
- **Domain** - `wega-api.velure.app.br`

**Key Labels:**
- `traefik.http.routers.wega-api-https.tls.certresolver=letsencrypt` - SSL certificate
- `traefik.http.middlewares.gzip.compress=true` - Response compression

### Environment Variables

Copy `.env.example` to `.env` and configure:

**Required:**
- `DB_PASSWORD` - PostgreSQL password (no default, must set)

**Optional (have defaults):**
- `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`
- `API_PORT` (default: 8080)
- `LOG_LEVEL` (default: info)

## Code Conventions

### Naming

- **Mixed-case SQL identifiers** - Table names like `"FABRICANTE"` are quoted because they're uppercase (legacy Oracle migration)
- **Portuguese domain terms** - Variables like `fabricante`, `aplicacao`, `filtros` match business domain
- **Repo suffix** - Repository structs end with `Repo` (e.g., `FabricanteRepo`)

### Error Handling

- **Service Layer** - Returns business logic errors as structured responses (e.g., status: "nao_encontrado")
- **Repository Layer** - Returns database errors directly (caller handles)
- **Handler Layer** - Converts errors to HTTP status codes (500 for unexpected errors)

### Logging

Uses structured logging (`log/slog`) with JSON output:
```go
slog.Info("message", "key", value)
slog.Error("error message", "error", err)
```

## Related Repositories

- [vm-oracle](https://github.com/icl00ud/vm-oracle) - VM configuration, PostgreSQL setup, database migration scripts

## API Usage Example

```bash
# Search filters for a 2020 VW Gol 1.0
curl -X POST http://localhost:8080/api/v1/filtros/buscar \
  -H "Content-Type: application/json" \
  -d '{"marca": "Volkswagen", "modelo": "Gol", "ano": "2020", "motor": "1.0"}'
```

**Expected Response:**
```json
{
  "status": "completo",
  "veiculo": {
    "marca": "Volkswagen",
    "modelo": "Gol",
    "ano": "2020",
    "motor": "1.0 3 Cil 12V",
    "descricao_completa": "Gol - 1.0 3 Cil 12V - 84 cv - Total Flex - (G7) - mecanico // 2019 -->"
  },
  "filtros": [
    {"codigo_wega": "WO780", "tipo": "Filtro do Oleo"},
    {"codigo_wega": "WAP0080", "tipo": "Filtro do Ar"}
  ],
  "total_filtros": 4
}
```
