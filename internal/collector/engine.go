package collector

import (
	"context"
	// "database/sql"
	"dbcollector/internal/config"
	"dbcollector/internal/database"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// dbUp indica se o coletor conseguiu conectar e pingar o banco (1=OK, 0=Falha)
	dbUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "db_collector_up",
		Help: "Status da conexão com o banco de dados (1 = Sucesso, 0 = Falha)",
	}, []string{"target_name", "driver", "database"})

	// dbMetric é o GaugeVec dinâmico que receberá todas as suas queries
	// Usamos chaves básicas e adicionamos as outras dinamicamente via .With(labels)
	dbMetric = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "db_collector_query_value",
		Help: "Valor retornado pela query (última coluna). Colunas anteriores são labels.",
	}, []string{"target_name", "query_name"})
)

type Engine struct {
	dbManager *database.Manager
}

func NewEngine(dbManager *database.Manager) *Engine {
	return &Engine{
		dbManager: dbManager,
	}
}

// Start inicia uma goroutine para cada query configurada
func (e *Engine) Start(cfg *config.Config) {
	slog.Info("Iniciando Engine de Coleta", "queries_count", len(cfg.Queries))
	for _, q := range cfg.Queries {
		go e.queryLoop(q, cfg.Targets)
	}
}

// queryLoop gerencia o ticker individual de cada query
func (e *Engine) queryLoop(q config.Query, allTargets []config.Target) {
	ticker := time.NewTicker(q.Interval)
	defer ticker.Stop()

	// Filtra quais alvos (bancos) essa query deve atingir (por Tag ou Nome)
	targets := filterTargets(q, allTargets)

	if len(targets) == 0 {
		slog.Warn("Query ignorada: nenhum target compatível encontrado", "query", q.Name)
		return
	}

	// O for range no ticker.C é mais limpo e performático para esse caso
	for range ticker.C {
		for _, t := range targets {
			// Timeout de segurança: 90% do intervalo da própria query
			timeout := time.Duration(float64(q.Interval) * 0.9)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			e.execute(ctx, t, q)
			cancel()
		}
	}
}

func (e *Engine) execute(ctx context.Context, target config.Target, q config.Query) {
	// 1. Tentar obter/criar conexão do pool
	db, err := e.dbManager.GetConnection(target.Driver, target.Host, target.Database, target.AuthRef)
	if err != nil {
		slog.Error("Falha ao obter conexão", "target", target.Name, "error", err)
		dbUp.WithLabelValues(target.Name, target.Driver, target.Database).Set(0)
		return
	}

	// 2. Validar saúde da conexão (Ping)
	if err := db.PingContext(ctx); err != nil {
		slog.Error("Banco inacessível (Ping falhou)", "target", target.Name, "error", err)
		dbUp.WithLabelValues(target.Name, target.Driver, target.Database).Set(0)
		return
	}

	// Se chegou aqui, o banco está respondendo
	dbUp.WithLabelValues(target.Name, target.Driver, target.Database).Set(1)

	// 3. Executar a Query SQL
	rows, err := db.QueryContext(ctx, q.SQL)
	if err != nil {
		slog.Error("Erro ao executar SQL", "target", target.Name, "query", q.Name, "error", err)
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	numCols := len(cols)

	for rows.Next() {
		// Preparar scanners para lidar com tipos desconhecidos (string, int, bytes, nulls)
		columns := make([]interface{}, numCols)
		columnPointers := make([]interface{}, numCols)
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			slog.Warn("Erro ao ler linha", "target", target.Name, "query", q.Name, "error", err)
			continue
		}

		// Montar as Labels do Prometheus
		labels := prometheus.Labels{
			"target_name": target.Name,
			"query_name":  q.Name,
		}

		var finalValue float64

		for i, colName := range cols {
			valStr := formatToString(columns[i])

			// REGRA: A última coluna é o VALOR da métrica (Float64)
			if i == numCols-1 {
				v, parseErr := strconv.ParseFloat(valStr, 64)
				if parseErr != nil {
					slog.Warn("A última coluna deve ser numérica", "query", q.Name, "col", colName, "val", valStr)
					continue
				}
				finalValue = v
			} else {
				// REGRA: Colunas anteriores viram labels (sempre String)
				labels[strings.ToLower(colName)] = valStr
			}
		}

		// Enviar para o Prometheus
		dbMetric.With(labels).Set(finalValue)
	}
}

// formatToString garante que qualquer tipo vindo do banco vire uma string limpa para a label
func formatToString(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// filterTargets cruza as Tags/Nomes da Query com os Targets disponíveis
func filterTargets(q config.Query, allTargets []config.Target) []config.Target {
	var filtered []config.Target
	targetMap := make(map[string]config.Target)

	// Filtro por Nomes Diretos
	for _, t := range allTargets {
		for _, name := range q.TargetNames {
			if t.Name == name {
				targetMap[t.Name] = t
			}
		}
		// Filtro por Tags
		for _, qTag := range q.TargetTags {
			for _, tTag := range t.Tags {
				if qTag == tTag {
					targetMap[t.Name] = t
				}
			}
		}
	}

	for _, t := range targetMap {
		filtered = append(filtered, t)
	}
	return filtered
}
