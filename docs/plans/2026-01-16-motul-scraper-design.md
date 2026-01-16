# Design: Motul Oil Specifications Scraper

**Data:** 2026-01-16
**Autor:** Claude Code + icl00ud
**Status:** Design aprovado, aguardando implementação

## Contexto

A API Wega possui catálogo completo de filtros automotivos, mas não tem especificações de fluidos (óleo motor, transmissão, freio). Essas informações são essenciais para completar o serviço de cotação via WhatsApp.

**Fonte dos Dados:** API da Motul (reverse-engineered via browser mobile)
**Catálogo Motul:** Padrão ouro do mercado de lubrificantes
**Dados Capturados:** `/Users/icl00ud/repos/scrape-wega-motul/captured_apis.json` (3.2MB)

## Objetivo

Criar um scraper em Go que extrai especificações de óleo da API Motul para os 49.034 veículos já cadastrados na tabela `APLICACAO`, armazenando em uma nova tabela `ESPECIFICACAO_TECNICA`.

**Prioridade:** Viscosidade e capacidade do óleo motor.

## Decisões de Arquitetura

### 1. Tecnologia e Localização

- **Linguagem:** Go (integrado ao projeto wega-catalog-api)
- **Deployment:** VM Ubuntu (140.238.178.70) rodando 24/7
- **Execução:** CLI standalone via tmux/nohup
- **Monitoramento:** Dashboard HTTP na porta 8081

**Justificativa:** VM elimina necessidade de manter MacBook ligado por 14 horas.

### 2. Escopo de Veículos

- **Total:** 49.034 veículos da tabela `APLICACAO`
- **Estratégia:** Buscar na Motul apenas veículos já existentes na Wega
- **Matching:** Fuzzy matching com confiança mínima de 80%

**Justificativa:** Garante compatibilidade total com catálogo existente.

### 3. Performance e Rate Limiting

- **Rate Limit:** 1 requisição/segundo
- **Duração Estimada:** ~14 horas
- **Persistência:** Commit incremental a cada 100 veículos
- **Retry:** Backoff exponencial (1s → 2s → 4s → 8s), até 5 tentativas

**Justificativa:** Abordagem conservadora minimiza risco de bloqueio pela Motul.

## Arquitetura do Sistema

```
┌─────────────────┐
│  CLI Command    │  ./motul-scraper --db-connection=...
│  (novo)         │
└────────┬────────┘
         │
         ├──> 1. Lê veículos da tabela APLICACAO (49.034)
         │
         ├──> 2. Para cada veículo:
         │        a) Busca marca/modelo/tipo na API Motul
         │        b) Fuzzy matching (confiança > 80%)
         │        c) Extrai especificações de óleo
         │        d) Rate limit: 1 req/segundo
         │
         └──> 3. Salva em ESPECIFICACAO_TECNICA
                 • Commit a cada 100 veículos
                 • Log de progresso e erros
                 • Checkpoint para resume
```

### Componentes Principais

```
wega-catalog-api/
├── cmd/motul-scraper/
│   └── main.go                    # Entry point, CLI flags
│
├── internal/
│   ├── client/
│   │   ├── motul.go              # HTTP client com rate limiting
│   │   └── rate_limiter.go
│   │
│   ├── matching/
│   │   ├── matcher.go            # Fuzzy matching engine
│   │   ├── normalizer.go         # Normalização de strings
│   │   └── extractor.go          # Extração de features (1.0, 12V, etc)
│   │
│   ├── parser/
│   │   └── motul_parser.go       # Parse JSON complexo da Motul
│   │
│   ├── scraper/
│   │   ├── service.go            # Orquestração do scraping
│   │   ├── progress.go           # Tracking de progresso
│   │   ├── checkpoint.go         # Save/load state
│   │   └── http_monitor.go       # Servidor HTTP /status
│   │
│   └── repository/
│       └── especificacao_repo.go # CRUD para ESPECIFICACAO_TECNICA
│
└── docs/plans/
    └── 2026-01-16-motul-scraper-design.md  # Este documento
```

## Schema do Banco

### Nova Tabela: ESPECIFICACAO_TECNICA

```sql
CREATE TABLE IF NOT EXISTS "ESPECIFICACAO_TECNICA" (
    "ID" SERIAL PRIMARY KEY,
    "CodigoAplicacao" INTEGER NOT NULL,
    "TipoFluido" VARCHAR(50) NOT NULL,      -- 'Motor', 'Transmissao', 'Freio'
    "Viscosidade" VARCHAR(50),              -- '5W-30', '0W-20', '75W-80'
    "Capacidade" VARCHAR(50),               -- '3.5 L', '2.0 litros'
    "Norma" VARCHAR(100),                   -- 'API SN', 'ACEA C3'
    "Recomendacao" VARCHAR(20),             -- 'Primaria', 'Alternativa'
    "Observacao" TEXT,
    "Fonte" VARCHAR(50) DEFAULT 'MotulAPI',
    "MotulVehicleTypeId" VARCHAR(100),      -- ID Motul (para re-scraping)
    "MatchConfidence" DECIMAL(5,2),         -- 0.85 = 85% confiança
    "CriadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    "AtualizadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_aplicacao
        FOREIGN KEY ("CodigoAplicacao")
        REFERENCES "APLICACAO" ("CodigoAplicacao")
        ON DELETE CASCADE
);

CREATE INDEX idx_specs_aplicacao ON "ESPECIFICACAO_TECNICA"("CodigoAplicacao");
CREATE INDEX idx_specs_tipo_fluido ON "ESPECIFICACAO_TECNICA"("TipoFluido");
CREATE INDEX idx_specs_fonte ON "ESPECIFICACAO_TECNICA"("Fonte");
```

**Campos Adicionais vs Proposta Original:**
- `Recomendacao` - Marca viscosidade primária vs alternativa
- `MotulVehicleTypeId` - Permite re-scraping futuro
- `MatchConfidence` - Auditoria de qualidade do matching

### Auto-Migration

O scraper cria a tabela automaticamente no primeiro run se ela não existir.

## Fluxo da API Motul

### Hierarquia de Endpoints

A API Motul segue um fluxo hierárquico descoberto via reverse engineering:

```
1. GET /oil-advisor/vehicle-brands?categoryId=CAR&locale=pt-BR&BU=Brazil
   └─> { brands: [{ id: "encodedId", name: "Volkswagen" }] }

2. GET /oil-advisor/vehicle-models?vehicleBrandId=encodedId&year=2020&locale=pt-BR&BU=Brazil
   └─> { models: [{ id: "encodedId", name: "Gol" }] }

3. GET /oil-advisor/vehicle-types?vehicleModelId=encodedId&locale=pt-BR&BU=Brazil
   └─> { types: [{ id: "157067", name: "Gol 1.0 12V (2019 - )" }] }

4. GET /lubricants/recommendations/vehicleTypeId.json?vehicleTypeId=157067
   └─> { pageProps: { vehicle: {...}, components: [...] } }
```

### Estratégia de Cache

- **Marcas:** Fetch completo no início, cache em memória (497 marcas)
- **Modelos:** Cache por marca+ano (reduz requests)
- **Tipos:** Sem cache (varia muito)
- **Specs:** Sem cache (dado final)

**Estimativa de Requests:** ~196.500 total (~4 requests por veículo em média)

## Algoritmo de Fuzzy Matching

### Desafio

Nomenclaturas diferentes entre Wega e Motul:
- **Wega:** "Gol - 1.0 3 Cil 12V - 84 cv - Total Flex - (G7) - mecanico // 2019 -->"
- **Motul:** "Gol 1.0 12V (2019 - )"

### Estratégia de Matching em 3 Níveis

**Nível 1: Marca (Exact Match)**
```
normalize("Volkswagen") == normalize("volkswagen") → Match ✅
```

**Nível 2: Modelo (Exact Match + Variações)**
```
"Gol" == "Gol" → Match ✅
"Gol G7" == "Gol" → Match ✅ (remove sufixos)
"HB20" == "HB 20" → Match ✅ (normaliza espaços)
```

**Nível 3: Motor/Tipo (Fuzzy Match com Scoring)**

```
Wega:  "1.0 3 Cil 12V - 84 cv - Total Flex"
       ↓ extrai features
       cilindrada: 1.0
       cilindros: 3
       valvulas: 12
       potencia: 84
       combustivel: flex

Motul: "1.0 12V (2019 - )"
       ↓ extrai features
       cilindrada: 1.0
       valvulas: 12

Scoring:
  ✓ Cilindrada match: +40 pontos (peso alto)
  ✓ Válvulas match:   +20 pontos
  ✗ Cilindros N/A:    +0 pontos
  ✗ Potência N/A:     +0 pontos
  ✓ Ano match:        +10 pontos

Total: 70/100 → 70% confiança → SKIP (< 80%)
```

### Pesos do Scoring

| Feature      | Pontos | Justificativa                    |
|--------------|--------|----------------------------------|
| Cilindrada   | 40     | Mais importante (1.0, 1.6, 2.0)  |
| Válvulas     | 20     | Diferencia versões (8V vs 16V)   |
| Cilindros    | 15     | Complementar (3, 4, 6 cil)       |
| Potência     | 15     | Validação extra                  |
| Ano          | 10     | Faixa de produção                |

### Regras de Aceitação

- **Confiança ≥ 80%** → Match aceito, dados importados
- **Confiança 60-79%** → Match duvidoso, loga para revisão, PULA
- **Confiança < 60%** → Sem match, PULA

### Casos Especiais

**Múltiplos matches acima de 80%:**
```
Escolhe o de maior score, loga todos para auditoria
```

**Veículos elétricos (sem cilindrada):**
```
Usa matching por kWh + modelo ao invés de cilindrada
```

## Parsing de Especificações

### Estrutura do JSON Motul

O JSON é complexo e usa referências numéricas:

```json
{
  "pageProps": {
    "vehicle": { "model": "208", "type": "1.0 6V Firefly Flex" },
    "components": [
      ["1", "2"],  // Array de referências
      {
        "category": "4",  // "4" = "motor"
        "recommendations": "5",
        "capacities": "3"
      },
      // ... 237 elementos misturando dados e referências
      "motor",      // String literal
      "2,54",       // Capacidade
      "0W-30"       // Viscosidade
    ]
  }
}
```

### Parser Inteligente

1. **Identifica componentes "motor"** no array
2. **Resolve referências cruzadas** (ex: "4" → "motor")
3. **Extrai com regex:**
   - **Viscosidade:** `\b\d+W-?\d+\b` (5W-30, 0W-20, 10W-40)
   - **Capacidade:** `\b\d+[,\.]\d+\b` + contexto "L"/"litros"
4. **Valida antes de salvar**

### Exemplo de Extração

**Input:**
```json
{
  "category": "motor",
  "recommendations": [{
    "name": "MOTUL 8100 ECO-CLEAN 0W-30"
  }],
  "capacities": "3,5"
}
```

**Output (SQL):**
```sql
INSERT INTO "ESPECIFICACAO_TECNICA"
  ("CodigoAplicacao", "TipoFluido", "Viscosidade", "Capacidade", "Fonte")
VALUES
  (12345, 'Motor', '0W-30', '3,5 L', 'MotulAPI');
```

### Múltiplas Viscosidades

Se a Motul retornar "5W-30 OU 0W-20":
- Salva 2 registros separados
- Marca primeira como `Recomendacao = 'Primaria'`
- Marca segunda como `Recomendacao = 'Alternativa'`

## Tratamento de Erros

### Categorias de Erros

**1. Erros de Rede (recuperáveis)**
- Timeout, 429, 500/502/503
- **Ação:** Retry com backoff (1s → 2s → 4s → 8s), até 5x

**2. Erros de Matching (skip)**
- Marca/modelo não encontrado, confiança < 80%
- **Ação:** Loga e pula

**3. Erros de Parsing (skip + alerta)**
- JSON malformado, estrutura inesperada
- **Ação:** Loga payload completo, pula veículo

**4. Erros de Banco (crítico)**
- Conexão perdida, disk full
- **Ação:** Para scraper, salva estado, alerta

### Circuit Breaker

Se 10 requisições consecutivas falharem:
1. Abre circuito (para requests)
2. Espera 5 minutos
3. Tenta 1 request (half-open)
4. Se sucesso → fecha circuito
5. Se falha → volta para open por +5min

### Checkpoint e Resume

**Arquivo:** `scraper.state.json`
```json
{
  "last_processed_id": 12500,
  "started_at": "2026-01-16T15:30:00Z",
  "stats": {
    "success": 11800,
    "failed": 200,
    "skipped": 500
  }
}
```

**Salvamento:**
- A cada 100 veículos
- Ao receber SIGINT (Ctrl+C)
- Antes de exit por erro crítico

**Retomar:** `./motul-scraper --resume`

## Monitoramento

### Dashboard HTTP (porta 8081)

**Endpoint:** `GET /status`

```json
{
  "status": "running",
  "started_at": "2026-01-16T15:30:00Z",
  "elapsed": "2h15m30s",
  "progress": {
    "total_vehicles": 49034,
    "processed": 10500,
    "success": 9800,
    "failed": 200,
    "skipped": 500,
    "percentage": 21.4
  },
  "matching_stats": {
    "exact_match": 8500,
    "fuzzy_match": 1300,
    "no_match": 700
  },
  "rate": {
    "current_rps": 0.98,
    "avg_time_per_vehicle": "1.02s"
  },
  "eta": {
    "remaining_vehicles": 38534,
    "estimated_completion": "2026-01-17T05:45:00Z",
    "time_remaining": "11h30m"
  }
}
```

**Acesso do Mac:**
```bash
# Atualiza a cada 5 segundos
watch -n 5 'curl -s http://140.238.178.70:8081/status | jq'
```

### Logs Estruturados

**Formato:**
```
[15:30:00] INFO  Starting scraper | total=49034 rate_limit=1s
[15:30:01] INFO  Progress 1/49034 (0.00%) | vehicle="VW Gol 1.0 2020" | match=exact | specs=4 | eta=13h45m
[15:31:40] INFO  Batch complete 100/49034 (0.20%) | success=95 failed=3 skipped=2 | eta=13h42m
[17:45:00] WARN  Match confidence low | vehicle="Uno 1.0" | confidence=0.75 | SKIPPED
```

**Acompanhar:**
```bash
ssh -i ~/Downloads/ssh-key-2025-12-23.key ubuntu@140.238.178.70 "tail -f scraper.log"
```

### Relatório Final

Ao concluir, gera relatório completo com:
- Duração total e taxa de sucesso
- Distribuição de viscosidades extraídas
- Estatísticas de matching (exact vs fuzzy)
- Performance (req/s, erros, retries)
- Ações recomendadas (revisar baixa confiança, etc)

**Arquivos:**
- `scraper.report.json` - Relatório estruturado
- `scraper.audit.log` - Matches para revisão manual
- Dashboard HTML em `/report`

## CLI e Configuração

### Uso Básico

```bash
# Primeira execução
./motul-scraper \
  --db-connection="postgres://postgres:...@o8cok8s4cg408cos4k0sowos:5432/postgres?sslmode=require" \
  --rate-limit=1s \
  --batch-size=100 \
  --http-port=8081

# Retomar após interrupção
./motul-scraper --resume

# Dry-run (testa sem salvar)
./motul-scraper --dry-run --limit=10
```

### Flags Principais

| Flag | Default | Descrição |
|------|---------|-----------|
| `--db-connection` | $DATABASE_URL | Connection string PostgreSQL |
| `--rate-limit` | 1s | Tempo entre requests |
| `--batch-size` | 100 | Commits a cada N veículos |
| `--http-port` | 8081 | Porta do dashboard |
| `--min-confidence` | 0.80 | Confiança mínima para match |
| `--resume` | false | Retoma de scraper.state.json |
| `--dry-run` | false | Testa sem salvar no banco |
| `--limit` | 0 | Processa apenas N veículos |
| `--filter-brand` | "" | Processa apenas uma marca |

### Deployment na VM

**1. Build no Mac:**
```bash
GOOS=linux GOARCH=amd64 go build -o motul-scraper ./cmd/motul-scraper
```

**2. Upload para VM:**
```bash
scp -i ~/Downloads/ssh-key-2025-12-23.key \
    motul-scraper \
    ubuntu@140.238.178.70:/home/ubuntu/
```

**3. Executar em tmux:**
```bash
ssh -i ~/Downloads/ssh-key-2025-12-23.key ubuntu@140.238.178.70

tmux new -s scraper
./motul-scraper \
  --db-connection="postgres://postgres:Erqn72G9...@o8cok8s4cg408cos4k0sowos:5432/postgres?sslmode=require" \
  --http-port=8081

# Detach: Ctrl+B, D
# Reconectar: tmux attach -t scraper
```

**4. Monitorar do Mac:**
```bash
watch -n 5 'curl -s http://140.238.178.70:8081/status | jq ".progress"'
```

## Estimativas

### Tempo

- **Total de veículos:** 49.034
- **Rate limit:** 1 req/segundo
- **Requests por veículo:** ~4 (marca, modelo, tipo, specs)
- **Duração estimada:** ~14 horas

### Dados

- **Registros esperados:** ~165.000
  - Óleo motor: 41.250 (1 por veículo)
  - Óleo transmissão: ~38.900 (95%)
  - Óleo freio: ~40.800 (99%)
  - Fluido arrefecimento: ~39.500 (96%)
  - Outros: ~4.550

- **Taxa de sucesso esperada:** 84-90%
  - Exact match: ~70%
  - Fuzzy match: ~15%
  - Sem match: ~10%
  - Erros: ~5%

## Riscos e Mitigações

| Risco | Impacto | Probabilidade | Mitigação |
|-------|---------|---------------|-----------|
| API Motul bloqueia IP | Alto | Baixo | Rate limit 1s, retry com backoff |
| API Motul muda estrutura | Médio | Baixo | Parser resiliente, logs detalhados |
| Conexão VM cai | Baixo | Baixo | Checkpoint a cada 100, --resume |
| Disco cheio | Médio | Baixo | Validação no startup, alertas |
| Matching ruim | Médio | Médio | Threshold 80%, auditoria manual |

## Próximos Passos

1. **Implementação** (estimativa: 2-3 dias de dev)
   - Estrutura base e CLI
   - Cliente HTTP Motul
   - Engine de fuzzy matching
   - Parser de especificações
   - Service e repository layers
   - Monitoramento HTTP

2. **Testes locais** (1 dia)
   - Dry-run com --limit=100
   - Validar matching em casos conhecidos
   - Testar resume após interrupção

3. **Deployment na VM** (algumas horas)
   - Build e upload
   - Execução inicial em tmux
   - Monitoramento por 1-2 horas

4. **Execução completa** (~14 horas)
   - Scraping dos 49k veículos
   - Monitoramento remoto
   - Revisão de auditoria

5. **Pós-scraping** (1 dia)
   - Análise do relatório
   - Revisão manual de baixa confiança
   - Integração com endpoint da API

## Referências

- **Repo scrape-wega-motul:** Dados capturados da API Motul
- **Schema proposal:** `/docs/SCHEMA_PROPOSAL.md`
- **VM:** 140.238.178.70 (user: ubuntu, SSH key: ssh-key-2025-12-23.key)
- **Banco interno:** `o8cok8s4cg408cos4k0sowos:5432`
