# Wega Catalog API

API Go para consulta do catalogo Wega Motors. Microservico preparado para integracao com N8N/Groq para automacao de orcamentos via WhatsApp.

## Stack

- **Go 1.22** (stdlib + chi router)
- **PostgreSQL** (pgx v5)
- **Docker** + Coolify

## Endpoints

### Health Check
```
GET /health
```

### Listar Fabricantes (Marcas de Veiculos)
```
GET /api/v1/fabricantes?tipo=veiculo
GET /api/v1/fabricantes?tipo=concorrente
```

### Buscar Filtros por Veiculo
```
POST /api/v1/filtros/buscar
Content-Type: application/json

{
  "marca": "Volkswagen",
  "modelo": "Gol",
  "ano": "2020",
  "motor": "1.0"
}
```

### Filtros por Aplicacao ID
```
GET /api/v1/filtros/aplicacao/{id}
```

### Tipos de Filtro
```
GET /api/v1/tipos-filtro
```

### Referencia Cruzada (Concorrente -> Wega)
```
GET /api/v1/referencia-cruzada?codigo=PH5949
```

## Desenvolvimento Local

```bash
# Copiar arquivo de ambiente
cp .env.example .env

# Editar .env com suas credenciais

# Rodar localmente
go run ./cmd/server

# Build
go build -o wega-api ./cmd/server
```

## Docker

```bash
# Build
docker build -t wega-catalog-api .

# Run
docker run -p 8080:8080 --env-file .env wega-catalog-api

# Ou com docker-compose (requer rede coolify)
docker-compose up -d
```

## Exemplos de Uso

### Buscar filtros para Gol 2020 1.0
```bash
curl -X POST http://localhost:8080/api/v1/filtros/buscar \
  -H "Content-Type: application/json" \
  -d '{"marca": "Volkswagen", "modelo": "Gol", "ano": "2020", "motor": "1.0"}'
```

### Referencia cruzada (Fram -> Wega)
```bash
curl "http://localhost:8080/api/v1/referencia-cruzada?codigo=PH5949"
```

## Fluxo de Integracao

```
Cliente WhatsApp
       |
       v
   N8N Webhook
       |
       v
   Groq LLM (extrai marca/modelo/ano/motor)
       |
       v
   POST /api/v1/filtros/buscar
       |
       v
   Lista de filtros Wega
       |
       v
   Cliente WhatsApp (orcamento)
```

## Licenca

MIT
