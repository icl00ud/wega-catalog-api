package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"wega-catalog-api/internal/config"
	"wega-catalog-api/internal/database"
	"wega-catalog-api/internal/handler"
	"wega-catalog-api/internal/repository"
	"wega-catalog-api/internal/service"
)

func main() {
	// Logger estruturado
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("iniciando wega-catalog-api")

	// Carregar config
	cfg := config.Load()

	// Conectar banco
	slog.Info("conectando ao banco de dados", "host", cfg.Database.Host, "database", cfg.Database.Name)
	db, err := database.NewPostgresPool(cfg.Database)
	if err != nil {
		slog.Error("falha ao conectar banco", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("conexao com banco estabelecida")

	// Repositorios
	fabricanteRepo := repository.NewFabricanteRepo(db)
	aplicacaoRepo := repository.NewAplicacaoRepo(db)
	produtoRepo := repository.NewProdutoRepo(db)
	referenciaRepo := repository.NewReferenciaRepo(db)

	// Service
	catalogoSvc := service.NewCatalogoService(
		fabricanteRepo, aplicacaoRepo, produtoRepo, referenciaRepo,
	)

	// Handlers
	healthHandler := handler.NewHealthHandler(db)
	fabricanteHandler := handler.NewFabricanteHandler(fabricanteRepo)
	filtroHandler := handler.NewFiltroHandler(catalogoSvc, produtoRepo)
	referenciaHandler := handler.NewReferenciaHandler(referenciaRepo)

	// Router
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Routes
	r.Get("/health", healthHandler.Check)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/fabricantes", fabricanteHandler.List)
		r.Get("/tipos-filtro", filtroHandler.ListTipos)
		r.Post("/filtros/buscar", filtroHandler.BuscarFiltros)
		r.Get("/filtros/aplicacao/{id}", filtroHandler.PorAplicacao)
		r.Get("/referencia-cruzada", referenciaHandler.Buscar)
	})

	// Server
	srv := &http.Server{
		Addr:         ":" + cfg.APIPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		slog.Info("servidor iniciado", "port", cfg.APIPort)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("erro no servidor", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("encerrando servidor...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("erro ao encerrar servidor", "error", err)
	}

	slog.Info("servidor encerrado")
}
