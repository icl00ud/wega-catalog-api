# Motul Oil Specifications Scraper

Automated scraper that enriches the Wega catalog with oil specifications from Motul's mobile API. Extracts viscosity, capacity, norms, and recommendations for all 49,034 vehicles in the APLICACAO table.

## Features

- **Rate-limited scraping** - 1 request/second with exponential backoff retry (1s → 2s → 4s → 8s)
- **Fuzzy matching** - Weighted scoring algorithm (80% confidence threshold) matches Wega vehicles to Motul data
- **Checkpoint/Resume** - Saves progress every 100 vehicles, can resume from interruptions
- **HTTP Monitoring** - Real-time progress endpoint with ETA, success/failure rates, matching stats
- **Graceful shutdown** - SIGINT/SIGTERM handling with clean database disconnection
- **Dry-run mode** - Test matching logic without writing to database

## Quick Start

### Local Testing

```bash
# Build the scraper
go build -o motul-scraper ./cmd/motul-scraper

# Dry-run with first 10 vehicles (test matching logic)
./motul-scraper \
  --db-host=o8cok8s4cg408cos4k0sowos \
  --db-port=5432 \
  --db-name=wega \
  --db-user=wega \
  --db-password='WegaCat_2026_Secure!' \
  --dry-run \
  --limit=10

# Production run with monitoring
./motul-scraper \
  --db-connection="postgres://wega:WegaCat_2026_Secure!@o8cok8s4cg408cos4k0sowos:5432/wega?sslmode=disable" \
  --rate-limit=1s \
  --http-port=8081
```

### VM Deployment (Recommended)

**Target:** `140.238.178.70` (24/7 execution, ~14 hours for full catalog)

**Step 1: Build for Linux**
```bash
# On your local machine (macOS/Linux)
GOOS=linux GOARCH=amd64 go build -o motul-scraper-linux ./cmd/motul-scraper
```

**Step 2: Upload to VM**
```bash
# SCP binary to VM
scp -i ~/.ssh/hetzner_vm motul-scraper-linux root@140.238.178.70:/root/motul-scraper

# SSH into VM
ssh -i ~/.ssh/hetzner_vm root@140.238.178.70
```

**Step 3: Run in tmux (background session)**
```bash
# Start tmux session
tmux new -s motul-scraper

# Inside tmux, run scraper
./motul-scraper \
  --db-connection="postgres://wega:WegaCat_2026_Secure!@o8cok8s4cg408cos4k0sowos:5432/wega?sslmode=disable" \
  --rate-limit=1s \
  --checkpoint-file=/root/motul-checkpoint.json \
  --http-port=8081 \
  --log-level=info

# Detach from tmux: Ctrl+B, then D
# Reattach later: tmux attach -t motul-scraper
```

## Monitoring

### Real-time Progress

```bash
# From VM or local machine (if port exposed)
curl http://140.238.178.70:8081/status | jq

# Example output:
{
  "current_vehicle": "Gol - 1.0 3 Cil 12V - 2020",
  "total_vehicles": 49034,
  "processed": 5420,
  "success": 4832,
  "failed": 102,
  "skipped": 486,
  "exact_match": 3201,
  "fuzzy_match": 1631,
  "no_match": 588,
  "percentage": 11.05,
  "eta": "12h 34m",
  "elapsed": "1h 30m 22s",
  "requests_per_sec": 1.02
}
```

### Health Check

```bash
curl http://140.238.178.70:8081/health

# Returns: "OK" (200) or "ERROR" (503)
```

### Watch Progress (live updates every 5s)

```bash
watch -n 5 'curl -s http://140.238.178.70:8081/status | jq ".percentage, .eta, .success, .failed"'
```

## Configuration Flags

### Database Connection

```
--db-connection     Full PostgreSQL connection string (recommended)
                    Example: postgres://user:pass@host:5432/dbname?sslmode=disable

--db-host          Database host (alternative to connection string)
--db-port          Database port (default: 5432)
--db-name          Database name (default: wega)
--db-user          Database user (default: wega)
--db-password      Database password (required if not in connection string)
```

### Scraping Behavior

```
--rate-limit       Request rate limit (default: 1s)
                   Examples: 1s, 500ms, 2s

--batch-size       Vehicles to process before checkpoint (default: 100)

--min-confidence   Fuzzy match confidence threshold (default: 0.80)
                   Range: 0.0 to 1.0 (80% = good balance)

--resume           Resume from specific vehicle ID
                   Example: --resume=25000

--limit            Process only N vehicles (testing)
                   Example: --limit=100

--dry-run          Test matching without database writes
```

### Monitoring & Persistence

```
--http-port        Monitoring server port (default: 8081)

--checkpoint-file  Checkpoint save path (default: scraper_checkpoint.json)

--log-level        Logging verbosity (default: info)
                   Options: debug, info, warn, error
```

## Architecture

### Matching Algorithm

The fuzzy matcher uses a 100-point weighted scoring system to match Wega vehicles with Motul API data:

| Feature       | Weight | Example Match                          |
|---------------|--------|----------------------------------------|
| Cilindrada    | 40pts  | 1.0L → 1000cc, 2.0L → 2000cc          |
| Válvulas      | 20pts  | 16V → 16 valves                        |
| Cilindros     | 15pts  | 4 Cil → 4 cylinders                    |
| Potência      | 15pts  | 120 cv → 120 hp                        |
| Ano           | 10pts  | 2020 → 2020                            |

**Example:**
- Wega: "Gol - 1.0 3 Cil 12V - 84 cv - 2020"
- Motul: "1.0 12V 3-Cylinder 84hp 2020-2023"
- Score: 40 (displacement) + 20 (valves) + 15 (cylinders) + 15 (power) + 10 (year) = **100 points (100%)**

Matches with scores ≥80% are accepted (configurable with `--min-confidence`).

### Database Schema

Creates `ESPECIFICACAO_TECNICA` table on first run:

```sql
CREATE TABLE "ESPECIFICACAO_TECNICA" (
    "ID" SERIAL PRIMARY KEY,
    "CodigoAplicacao" INTEGER NOT NULL REFERENCES "APLICACAO"("CodigoAplicacao"),
    "TipoFluido" VARCHAR(100),
    "Viscosidade" VARCHAR(50),
    "Capacidade" VARCHAR(50),
    "Norma" TEXT,
    "Recomendacao" TEXT,
    "Fonte" VARCHAR(50) DEFAULT 'Motul API',
    "MotulVehicleTypeId" VARCHAR(100),
    "MatchConfidence" DECIMAL(5,2),
    "CriadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    "AtualizadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Checkpoint Format

Progress is saved to JSON every 100 vehicles (or on shutdown):

```json
{
  "last_processed_id": 25000,
  "started_at": "2026-01-17T10:30:00Z",
  "saved_at": "2026-01-17T15:45:12Z",
  "stats": {
    "total": 49034,
    "processed": 25000,
    "success": 22400,
    "failed": 850,
    "skipped": 1750,
    "exact_match": 15200,
    "fuzzy_match": 7200,
    "no_match": 2600
  }
}
```

## Expected Results

### Timeline

- **Full run:** ~14 hours (49,034 vehicles @ 1 req/s)
- **Partial test:** 10 vehicles = ~10 seconds
- **Hourly throughput:** ~3,600 vehicles

### Match Rates (estimated)

Based on fuzzy matching algorithm:

- **Exact matches:** ~60-70% (complete feature alignment)
- **Fuzzy matches:** ~20-25% (80-99% confidence scores)
- **No matches:** ~5-10% (rare/discontinued vehicles)
- **Failures:** <5% (API errors, rate limits, timeouts)

### Data Completeness

Priority fields (most vehicles):
- ✅ Viscosidade (oil viscosity): ~95% coverage
- ✅ Capacidade (oil capacity): ~90% coverage
- ✅ Norma (specifications): ~85% coverage
- ✅ Recomendacao (Motul product): ~95% coverage

Secondary fields (some vehicles):
- 🟡 TipoFluido: ~70% (transmission/differential oils)
- 🟡 Observacao: ~40% (special notes)

## Troubleshooting

### Scraper Stops Unexpectedly

```bash
# Check if process is running
ps aux | grep motul-scraper

# Check tmux session
tmux ls
tmux attach -t motul-scraper

# Review checkpoint file
cat /root/motul-checkpoint.json | jq

# Resume from last checkpoint
./motul-scraper --db-connection="..." --resume=<last_id>
```

### High Failure Rate

```bash
# Reduce rate limit to avoid throttling
./motul-scraper --rate-limit=2s ...

# Lower confidence threshold (more lenient matching)
./motul-scraper --min-confidence=0.70 ...

# Enable debug logging
./motul-scraper --log-level=debug ...
```

### Database Connection Issues

```bash
# Test connection from VM
psql "postgres://wega:WegaCat_2026_Secure!@o8cok8s4cg408cos4k0sowos:5432/wega?sslmode=disable"

# Check if migration ran
psql -c 'SELECT COUNT(*) FROM "ESPECIFICACAO_TECNICA";'

# Verify foreign keys
psql -c '\d "ESPECIFICACAO_TECNICA"'
```

### Memory Issues (VM)

```bash
# Monitor memory usage
watch -n 5 free -h

# Reduce batch size (less memory buffering)
./motul-scraper --batch-size=50 ...
```

## Post-Scraping Validation

### Verify Data Quality

```sql
-- Total specifications inserted
SELECT COUNT(*) FROM "ESPECIFICACAO_TECNICA";

-- Match confidence distribution
SELECT
    CASE
        WHEN "MatchConfidence" >= 0.95 THEN 'Exact (95-100%)'
        WHEN "MatchConfidence" >= 0.80 THEN 'Fuzzy (80-95%)'
        ELSE 'Low (<80%)'
    END AS match_type,
    COUNT(*) as count,
    ROUND(AVG("MatchConfidence") * 100, 2) as avg_confidence
FROM "ESPECIFICACAO_TECNICA"
GROUP BY match_type;

-- Vehicles with oil specs
SELECT COUNT(DISTINCT "CodigoAplicacao")
FROM "ESPECIFICACAO_TECNICA";

-- Most common viscosities
SELECT "Viscosidade", COUNT(*) as count
FROM "ESPECIFICACAO_TECNICA"
WHERE "Viscosidade" IS NOT NULL
GROUP BY "Viscosidade"
ORDER BY count DESC
LIMIT 10;
```

### Integration with API

After scraping completes, the specifications are available via the main API:

```bash
# Get vehicle with oil specs
curl http://wega-api.velure.app.br/api/v1/filtros/buscar \
  -H "Content-Type: application/json" \
  -d '{"marca": "Volkswagen", "modelo": "Gol", "ano": "2020"}'

# Response now includes:
{
  "status": "completo",
  "veiculo": { ... },
  "filtros": [ ... ],
  "especificacoes": [
    {
      "tipo_fluido": "Óleo do Motor",
      "viscosidade": "5W-30",
      "capacidade": "3.6L",
      "norma": "API SN, ACEA A3/B4",
      "recomendacao": "Motul 8100 X-clean 5W-30",
      "match_confidence": 98.5
    }
  ]
}
```

## Maintenance

### Re-run After Database Updates

If new vehicles are added to `APLICACAO` table:

```bash
# Scrape only new vehicles (resume from highest ID)
./motul-scraper --resume=<max_id_from_last_run> ...

# Or use limit to test new additions
./motul-scraper --limit=100 --resume=49000 ...
```

### Update Matching Logic

If fuzzy matching needs tuning:

1. Edit weights in `internal/matching/matcher.go`
2. Rebuild: `GOOS=linux GOARCH=amd64 go build -o motul-scraper-linux ./cmd/motul-scraper`
3. Upload to VM: `scp -i ~/.ssh/hetzner_vm motul-scraper-linux root@140.238.178.70:/root/motul-scraper`
4. Re-run with `--dry-run --limit=100` to test changes

### Clean Restart

```bash
# Remove checkpoint (start from beginning)
rm /root/motul-checkpoint.json

# Truncate existing data
psql -c 'TRUNCATE "ESPECIFICACAO_TECNICA" CASCADE;'

# Run scraper
./motul-scraper --db-connection="..." ...
```

## License

Part of the Wega Catalog API project. Internal use only.
