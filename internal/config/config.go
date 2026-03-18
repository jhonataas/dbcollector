package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config unifica todas as configurações lidas das pastas
type Config struct {
	ServerPort string        `yaml:"server_port"`
	LogLevel   string        `yaml:"log_level"`
	Auth       AuthConfig    `yaml:"auth"`
	Targets    []Target      `yaml:"targets"`
	Queries    []Query       `yaml:"queries"`
}

type AuthConfig struct {
	Method      string            `yaml:"method"`
	CacheTTLString    string     `yaml:"cache_ttl"`
	BeyondTrust    BeyondTrustConfig `yaml:"beyondtrust"`
	LocalCredentials  []LocalCredential `yaml:"local_credentials"`
}

// Para facilitar, você pode criar um método auxiliar na struct:
func (a AuthConfig) GetCacheTTL() time.Duration {
    d, err := time.ParseDuration(a.CacheTTLString)
    if err != nil {
        return 30 * time.Minute // Default de segurança
    }
    return d
}

type BeyondTrustConfig struct {
	URL          string `yaml:"api_url"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type LocalCredential struct {
	ID   string `yaml:"id"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type Target struct {
	Name     string   `yaml:"name"`
	Driver   string   `yaml:"driver"`
	Host     string   `yaml:"host"`
	Database string   `yaml:"database"`
	AuthRef  string   `yaml:"auth_ref"`
	Tags     []string `yaml:"tags"` // Labels para filtro de queries
}

type Query struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Interval    time.Duration `yaml:"interval"`     // Agora cada query tem seu tempo
	TargetTags  []string      `yaml:"target_tags"`  // Rodar em bancos com estas tags
	TargetNames []string      `yaml:"target_names"` // Rodar nestes bancos específicos
	SQL         string        `yaml:"sql"`
}

// Load percorre a estrutura de pastas e monta o objeto Config único
func Load(basePath string) (*Config, error) {
	cfg := &Config{}

	// 1. Ler arquivo GLOBAL (Porta, Log, Auth)
	globalFile := filepath.Join(basePath, "global.yaml")
	if err := loadYaml(globalFile, cfg); err != nil {
		return nil, fmt.Errorf("erro no global.yaml: %w", err)
	}

	// 2. Ler todos os Bancos em config/dbs/*.yaml
	dbPattern := filepath.Join(basePath, "dbs", "*.yaml")
	dbFiles, _ := filepath.Glob(dbPattern)
	for _, f := range dbFiles {
		var tmp struct { Targets []Target `yaml:"targets"` }
		if err := loadYaml(f, &tmp); err != nil {
			return nil, fmt.Errorf("erro no arquivo %s: %w", f, err)
		}
		cfg.Targets = append(cfg.Targets, tmp.Targets...)
	}

	// 3. Ler todas as Queries em config/queries/*.yaml
	queryPattern := filepath.Join(basePath, "queries", "*.yaml")
	queryFiles, _ := filepath.Glob(queryPattern)
	for _, f := range queryFiles {
		var tmp struct { Queries []Query `yaml:"queries"` }
		if err := loadYaml(f, &tmp); err != nil {
			return nil, fmt.Errorf("erro no arquivo %s: %w", f, err)
		}
		cfg.Queries = append(cfg.Queries, tmp.Queries...)
	}

	// Validações básicas de Sênior
	if cfg.ServerPort == "" { cfg.ServerPort = "42000" }
	
	return cfg, nil
}

// loadYaml é um helper para ler, expandir ENV e dar Unmarshal
func loadYaml(path string, out interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Permite usar ${VAR} nos YAMLs
	expanded := os.ExpandEnv(string(data))
	return yaml.Unmarshal([]byte(expanded), out)
}