package model

import "time"

// ScraperFalha represents a failed scraper attempt for retry
type ScraperFalha struct {
	ID               int        `json:"id"`
	CodigoAplicacao  int        `json:"codigo_aplicacao"`
	TipoErro         string     `json:"tipo_erro"`
	MensagemErro     string     `json:"mensagem_erro"`
	Tentativas       int        `json:"tentativas"`
	UltimaTentativa  time.Time  `json:"ultima_tentativa"`
	ProximaTentativa *time.Time `json:"proxima_tentativa,omitempty"`
	Resolvido        bool       `json:"resolvido"`
	ResolvidoEm      *time.Time `json:"resolvido_em,omitempty"`
	CriadoEm         time.Time  `json:"criado_em"`
}

// Error types for categorization
const (
	ErroTipoRateLimit           = "rate_limit"
	ErroTipoModeloNaoEncontrado = "modelo_nao_encontrado"
	ErroTipoAPIMotul            = "api_motul"
	ErroTipoAPIGroq             = "api_groq"
	ErroTipoRede                = "rede"
	ErroTipoParse               = "parse"
	ErroTipoDesconhecido        = "desconhecido"
)

// ClassifyError categorizes an error string into a type
func ClassifyError(errMsg string) string {
	switch {
	case contains(errMsg, "rate limit", "429", "too many requests"):
		return ErroTipoRateLimit
	case contains(errMsg, "model not found", "LLM indicated no match"):
		return ErroTipoModeloNaoEncontrado
	case contains(errMsg, "Motul API"):
		return ErroTipoAPIMotul
	case contains(errMsg, "Groq API"):
		return ErroTipoAPIGroq
	case contains(errMsg, "connection", "timeout", "network", "dial"):
		return ErroTipoRede
	case contains(errMsg, "parse", "invalid"):
		return ErroTipoParse
	default:
		return ErroTipoDesconhecido
	}
}

// contains checks if s contains any of the substrings (case-insensitive)
func contains(s string, substrs ...string) bool {
	sLower := toLower(s)
	for _, sub := range substrs {
		if indexOf(sLower, toLower(sub)) >= 0 {
			return true
		}
	}
	return false
}

// toLower converts string to lowercase (simple ASCII)
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// indexOf returns index of substr in s, or -1 if not found
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
