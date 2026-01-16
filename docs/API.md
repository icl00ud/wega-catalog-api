# Wega Catalog API - Documentacao

> **STATUS: IMPLEMENTADO**
> Repositorio: https://github.com/icl00ud/wega-catalog-api

Microservico Go para consulta do catalogo Wega Motors, preparado para integracao com N8N/Groq para automacao de orcamentos via WhatsApp.

## Repositorios do Projeto

| Repositorio | Descricao |
|-------------|-----------|
| [vm-oracle](https://github.com/icl00ud/vm-oracle) | Configuracoes da VM, scripts de migracao, setup PostgreSQL |
| [wega-catalog-api](https://github.com/icl00ud/wega-catalog-api) | API Go do catalogo Wega (este documento) |

## Visao Geral do Fluxo

```
Cliente WhatsApp
       |
       v
   N8N Webhook --> recebe mensagem "quero orcamento Gol 2020 1.0"
       |
       v
   Groq LLM --> extrai: {marca, modelo, ano, motor}
       |
       v
   Wega API --> POST /api/v1/filtros/buscar
       |
       v
   PostgreSQL --> busca filtros compativeis
       |
       v
   Resposta --> lista de filtros Wega
       |
       v
   Cliente WhatsApp <-- orcamento formatado
```

## Stack Tecnologica

| Componente | Tecnologia |
|------------|------------|
| Linguagem | Go 1.22 (stdlib + chi router) |
| Banco de Dados | PostgreSQL 17 (pgx v5) |
| Deploy | Docker + Coolify |
| Proxy | Traefik (SSL/Let's Encrypt) |
| LLM | Groq |
| Automacao | N8N |

## Estrutura do Projeto (wega-catalog-api)

```
wega-catalog-api/
├── cmd/
│   └── server/
│       └── main.go                 # Entry point
├── internal/
│   ├── config/
│   │   └── config.go               # Configuracoes (env vars)
│   ├── database/
│   │   └── postgres.go             # Pool de conexoes pgx
│   ├── handler/
│   │   ├── health.go               # Health check
│   │   ├── fabricante.go           # Listar marcas
│   │   ├── filtro.go               # Buscar filtros
│   │   └── referencia.go           # Referencia cruzada
│   ├── model/
│   │   ├── fabricante.go           # Struct Fabricante
│   │   ├── aplicacao.go            # Struct Aplicacao
│   │   ├── produto.go              # Struct Produto
│   │   └── response.go             # Responses padronizados
│   ├── repository/
│   │   ├── fabricante_repo.go      # Queries fabricante
│   │   ├── aplicacao_repo.go       # Queries aplicacao
│   │   ├── produto_repo.go         # Queries produto
│   │   └── referencia_repo.go      # Queries ref cruzada
│   └── service/
│       └── catalogo_service.go     # Logica de negocio principal
├── Dockerfile
├── docker-compose.yaml
├── go.mod
└── go.sum
```

## Endpoints da API

| Metodo | Endpoint | Descricao |
|--------|----------|-----------|
| GET | `/health` | Health check |
| GET | `/api/v1/fabricantes` | Listar marcas de veiculos |
| GET | `/api/v1/fabricantes?tipo=concorrente` | Listar marcas concorrentes |
| GET | `/api/v1/tipos-filtro` | Listar tipos de filtro |
| POST | `/api/v1/filtros/buscar` | **Buscar filtros por veiculo** |
| GET | `/api/v1/filtros/aplicacao/{id}` | Filtros por ID de aplicacao |
| GET | `/api/v1/referencia-cruzada?codigo=XX` | Conversao concorrente → Wega |

### Buscar Filtros por Veiculo (ENDPOINT PRINCIPAL)

```http
POST /api/v1/filtros/buscar
Content-Type: application/json
```

**Request Body:**
```json
{
  "marca": "Volkswagen",
  "modelo": "Gol",
  "ano": "2020",
  "motor": "1.0"
}
```

**Possiveis Respostas:**

| Status | Descricao |
|--------|-----------|
| `completo` | Filtros encontrados com sucesso |
| `incompleto` | Faltam informacoes (ano, motor) - retorna opcoes disponiveis |
| `multiplos` | Varios veiculos encontrados - usuario deve escolher |
| `nao_encontrado` | Veiculo nao existe no catalogo |

**Response - Sucesso:**
```json
{
  "status": "completo",
  "veiculo": {
    "marca": "Volkswagen",
    "modelo": "Gol",
    "ano": "2020",
    "motor": "1.0 3 Cil 12V",
    "descricao_completa": "Gol - 1.0 3 Cil 12V - 84 cv - Total Flex - (G7 - Track) - mecanico // 2019 -->"
  },
  "filtros": [
    {
      "codigo_wega": "WO780",
      "tipo": "Filtro do Oleo",
      "foto_url": "https://wega.com.br/fotos/WO780.jpg"
    },
    {
      "codigo_wega": "WAP0080",
      "tipo": "Filtro do Ar",
      "foto_url": "https://wega.com.br/fotos/WAP0080.jpg"
    }
  ],
  "total_filtros": 4
}
```

**Response - Informacao Incompleta:**
```json
{
  "status": "incompleto",
  "mensagem": "Preciso de mais informacoes para encontrar os filtros corretos.",
  "campos_faltantes": ["ano", "motor"],
  "opcoes_disponiveis": {
    "anos": ["2018", "2019", "2020", "2021", "2022"],
    "motores": ["1.0 3 Cil 12V", "1.6 4 Cil 16V"]
  }
}
```

**Response - Multiplas Opcoes:**
```json
{
  "status": "multiplos",
  "mensagem": "Encontrei varios veiculos compativeis. Qual destes?",
  "opcoes": [
    {
      "id": 370461,
      "descricao": "Gol - 1.0 4 Cil 8V - 76 cv - Total Flex - (G5) - mecanico // 08 -- 10"
    },
    {
      "id": 412345,
      "descricao": "Gol - 1.0 3 Cil 12V - 84 cv - Total Flex - (G7) - mecanico // 2019 -->"
    }
  ]
}
```

### Referencia Cruzada (Concorrente -> Wega)

```http
GET /api/v1/referencia-cruzada?codigo=PH5949
```

**Response:**
```json
{
  "codigo_pesquisado": "PH5949",
  "marca_concorrente": "Fram",
  "equivalentes_wega": [
    {
      "codigo_wega": "WO780",
      "descricao": "Filtro de Oleo",
      "tipo": "Filtro do Oleo"
    }
  ]
}
```

## Banco de Dados

### Dados de Conexao

```
Host: o8cok8s4cg408cos4k0sowos (container Docker)
Port: 5432
Database: wega
User: wega
Password: WegaCat_2026_Secure!
SSL Mode: disable (rede interna Docker)
```

### Tabelas Principais

| Tabela | Registros | Descricao |
|--------|-----------|-----------|
| FABRICANTE | 497 | Marcas de veiculos e concorrentes |
| APLICACAO | 49.034 | Modelos/versoes de veiculos |
| PRODUTO | 3.432 | Pecas/filtros Wega |
| PRODUTO_APLICACAO | 51.426 | Relacionamento N:N |
| SUBGRUPOPRODUTO | 29 | Tipos de filtro (Oleo, Ar, etc) |
| REFERENCIACRUZADA | 34.243 | Equivalencia com concorrentes |

### Indices Recomendados

```sql
CREATE INDEX IF NOT EXISTS idx_aplicacao_fabricante ON "APLICACAO"("CodigoFabricante");
CREATE INDEX IF NOT EXISTS idx_aplicacao_descricao ON "APLICACAO" USING gin(to_tsvector('portuguese', "DescricaoAplicacao"));
CREATE INDEX IF NOT EXISTS idx_produto_aplicacao_aplicacao ON "PRODUTO_APLICACAO"("CodigoAplicacao");
CREATE INDEX IF NOT EXISTS idx_produto_aplicacao_produto ON "PRODUTO_APLICACAO"("CodigoProduto");
CREATE INDEX IF NOT EXISTS idx_referencia_pesq ON "REFERENCIACRUZADA"("NumeroProdutoPesq");
```

## Deploy

### Variaveis de Ambiente

```env
# Database
DB_HOST=o8cok8s4cg408cos4k0sowos
DB_PORT=5432
DB_NAME=wega
DB_USER=wega
DB_PASSWORD=WegaCat_2026_Secure!
DB_SSLMODE=disable

# API
API_PORT=8080
LOG_LEVEL=info
```

### Docker

```bash
# Build
docker build -t wega-catalog-api .

# Run
docker run -p 8080:8080 --env-file .env wega-catalog-api

# Ou com docker-compose (requer rede coolify)
docker-compose up -d
```

### Coolify

A API esta configurada para deploy no Coolify com:
- Traefik como proxy reverso
- SSL automatico via Let's Encrypt
- Dominio: `wega-api.velure.app.br`

## Exemplos de Uso

### cURL

```bash
# Health check
curl https://wega-api.velure.app.br/health

# Buscar filtros para Gol 2020 1.0
curl -X POST https://wega-api.velure.app.br/api/v1/filtros/buscar \
  -H "Content-Type: application/json" \
  -d '{"marca": "Volkswagen", "modelo": "Gol", "ano": "2020", "motor": "1.0"}'

# Referencia cruzada (Fram -> Wega)
curl "https://wega-api.velure.app.br/api/v1/referencia-cruzada?codigo=PH5949"

# Listar fabricantes de veiculos
curl https://wega-api.velure.app.br/api/v1/fabricantes

# Listar tipos de filtro
curl https://wega-api.velure.app.br/api/v1/tipos-filtro
```

### N8N HTTP Request Node

```json
{
  "method": "POST",
  "url": "https://wega-api.velure.app.br/api/v1/filtros/buscar",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "marca": "{{ $json.marca }}",
    "modelo": "{{ $json.modelo }}",
    "ano": "{{ $json.ano }}",
    "motor": "{{ $json.motor }}"
  }
}
```

## Proximos Passos

- [x] ~~Implementar API Go~~
- [x] ~~Criar repositorio no GitHub~~
- [ ] Deploy no Coolify
- [ ] Configurar N8N workflow
- [ ] Integrar WhatsApp (API oficial)
- [ ] Adicionar precos (integrar com banco da empresa)
- [ ] Criar fluxo conversacional com Groq
