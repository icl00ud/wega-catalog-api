# Proposta de Schema: Especificacoes Tecnicas

Para armazenar os dados de oleo e fluidos que vamos extrair (via App ou Scraper), precisamos de uma nova tabela vinculada aos veiculos.

## Nova Tabela: `ESPECIFICACAO_TECNICA`

```sql
CREATE TABLE IF NOT EXISTS "ESPECIFICACAO_TECNICA" (
    "ID" SERIAL PRIMARY KEY,
    "CodigoAplicacao" INTEGER NOT NULL, -- FK para tabela APLICACAO
    "TipoFluido" VARCHAR(50) NOT NULL,  -- Ex: 'Motor', 'Cambio Manual', 'Radiador'
    "Viscosidade" VARCHAR(50),          -- Ex: '5W-30', '75W-80'
    "Capacidade" VARCHAR(50),           -- Ex: '3.5 litros', '5.2 L'
    "Norma" VARCHAR(100),               -- Ex: 'API SN', 'ACEA A3/B4'
    "IntervaloTroca" VARCHAR(100),      -- Ex: '10.000 km / 1 ano'
    "Observacao" TEXT,
    "Fonte" VARCHAR(50) DEFAULT 'Wega', -- Ex: 'MotulApp', 'Manual', 'Wega'
    "AtualizadoEm" TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT fk_aplicacao 
        FOREIGN KEY ("CodigoAplicacao") 
        REFERENCES "APLICACAO" ("CodigoAplicacao")
);

-- Indices para busca rapida
CREATE INDEX idx_specs_aplicacao ON "ESPECIFICACAO_TECNICA"("CodigoAplicacao");
```

## Exemplo de Dados

| ID | CodigoAplicacao | TipoFluido | Viscosidade | Capacidade | Norma | Fonte |
|----|-----------------|------------|-------------|------------|-------|-------|
| 1  | 12345 (Peugeot) | Motor | 0W-20 | 3.2 L | PSA B71 2010 | MotulApp |
| 2  | 12345 (Peugeot) | Cambio | 75W | 2.0 L | API GL-4 | MotulApp |
| 3  | 12345 (Peugeot) | Freio | DOT 4 | - | - | MotulApp |

## Modelo Go (`internal/model/especificacao.go`)

```go
package model

type EspecificacaoTecnica struct {
    ID              int    `json:"id"`
    CodigoAplicacao int    `json:"codigo_aplicacao"`
    TipoFluido      string `json:"tipo_fluido"`
    Viscosidade     string `json:"viscosidade,omitempty"`
    Capacidade      string `json:"capacidade,omitempty"`
    Norma           string `json:"norma,omitempty"`
    IntervaloTroca  string `json:"intervalo_troca,omitempty"`
    Observacao      string `json:"observacao,omitempty"`
    Fonte           string `json:"fonte"`
}
```
