package database

import (
	"database/sql"
	"dbcollector/internal/auth"
	"fmt"
	"sync"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/sijms/go-ora/v2"
)

type Manager struct {
	auth     auth.Provider
	conns    map[string]*sql.DB // Cache de pools de conexão
	mu       sync.RWMutex       // Mutex para evitar race conditions no mapa
}

func NewManager(auth auth.Provider) *Manager {
	return &Manager{
		auth:  auth,
		conns: make(map[string]*sql.DB),
	}
}

func (m *Manager) GetConnection(driver, host, dbName, authRef string) (*sql.DB, error) {
	connKey := fmt.Sprintf("%s|%s|%s", driver, host, dbName)

	// 1. Verificar se já temos essa conexão aberta
	m.mu.RLock()
	if db, ok := m.conns[connKey]; ok {
		m.mu.RUnlock()
		return db, nil
	}
	m.mu.RUnlock()

	// 2. Se não tem, criar nova (Lock total para escrita)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check para evitar que outra goroutine tenha criado enquanto esperávamos o Lock
	if db, ok := m.conns[connKey]; ok {
		return db, nil
	}

	user, pass, err := m.auth.GetCredentials(authRef)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter credenciais: %w", err)
	}

	var dsn string
	switch driver {
	case "sqlserver":
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s?database=%s", user, pass, host, dbName)
	case "postgres":
		dsn = fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", user, pass, host, dbName)
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s", user, pass, host, dbName)
	case "oracle":
		dsn = fmt.Sprintf("oracle://%s:%s@%s/%s", user, pass, host, dbName)
	default:
		return nil, fmt.Errorf("driver não suportado: %s", driver)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}

	// Configurações de resiliência
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Minute * 5)

	m.conns[connKey] = db
	return db, nil
}