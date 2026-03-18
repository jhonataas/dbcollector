package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dbcollector/internal/auth"
	"dbcollector/internal/collector"
	"dbcollector/internal/config"
	"dbcollector/internal/database"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 1. Setup Log Estruturado
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// 2. Carregar toda a estrutura de pastas de CONFIG
	// Passamos o caminho da pasta onde estão global.yaml, dbs/ e queries/
	cfg, err := config.Load("./config")
	if err != nil {
		slog.Error("Falha crítica ao carregar configurações", "error", err)
		os.Exit(1)
	}
	slog.Info("Configurações carregadas", 
		"targets", len(cfg.Targets), 
		"queries", len(cfg.Queries),
		"port", cfg.ServerPort)

	// 3. Inicializar Autenticação (BeyondTrust ou Local)
	var authProvider auth.Provider
	if cfg.Auth.Method == "beyondtrust" {
		authProvider = auth.NewBeyondTrustProvider(
			cfg.Auth.BeyondTrust.URL,
			cfg.Auth.BeyondTrust.ClientSecret,
			cfg.Auth.GetCacheTTL(),
		)
	} else {
		authProvider = auth.NewLocalProvider(cfg.Auth.LocalCredentials)
	}

	// 4. Inicializar Gerenciador de Banco de Dados
	dbManager := database.NewManager(authProvider)

	// 5. Inicializar e Iniciar a Engine de Coleta
	engine := collector.NewEngine(dbManager)
	engine.Start(cfg)

	// 6. Exposição do Endpoint de Métricas
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: mux,
	}

	// 7. Execução e Graceful Shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Erro no servidor HTTP", "error", err)
		}
	}()

	// Captura sinais de parada (Ctrl+C, Docker stop)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Encerrando coletor graciosamente...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Erro ao encerrar servidor", "error", err)
	}
	
	slog.Info("Serviço finalizado com sucesso.")
}