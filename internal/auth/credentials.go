package auth

import (
	"dbcollector/internal/config" // Importante para o construtor local
	"fmt"
	"sync"
	"time"
	"log/slog"
)

type Provider interface {
	GetCredentials(targetID string) (user, pass string, err error)
}

type BeyondTrustProvider struct {
	apiURL   string
	apiKey   string
	cache    sync.Map
	ttl      time.Duration
}

type cachedCreds struct {
	user      string
	pass      string
	expiresAt time.Time
}

func NewBeyondTrustProvider(url, key string, cacheDuration time.Duration) *BeyondTrustProvider {
	return &BeyondTrustProvider{
		apiURL: url,
		apiKey: key,
		ttl:    cacheDuration,
	}
}

func (p *BeyondTrustProvider) GetCredentials(targetID string) (string, string, error) {
	if val, ok := p.cache.Load(targetID); ok {
		cred := val.(cachedCreds)
		if time.Now().Before(cred.expiresAt) {
			return cred.user, cred.pass, nil
		}
	}

	slog.Info("Buscando nova senha no BeyondTrust", "target", targetID)
	
	// Implementação real simplificada (exemplo)
	user, pass, err := p.fetchFromBTAPI(targetID)
	if err != nil {
		return "", "", err
	}

	p.cache.Store(targetID, cachedCreds{
		user:      user,
		pass:      pass,
		expiresAt: time.Now().Add(p.ttl),
	})

	return user, pass, nil
}

// Simulação de chamada API
func (p *BeyondTrustProvider) fetchFromBTAPI(targetID string) (string, string, error) {
	// Aqui entraria seu http.Client fazendo o POST para o BeyondTrust
	// Por enquanto, simulamos sucesso.
	return "db_user_api", "db_pass_api", nil
}

// --- LOCAL PROVIDER ---

type LocalProvider struct {
	creds map[string]config.LocalCredential
}

func NewLocalProvider(users []config.LocalCredential) *LocalProvider {
	m := make(map[string]config.LocalCredential)
	for _, u := range users {
		m[u.ID] = u
	}
	return &LocalProvider{creds: m}
}

func (l *LocalProvider) GetCredentials(targetID string) (string, string, error) {
	if cred, ok := l.creds[targetID]; ok {
		return cred.User, cred.Pass, nil
	}
	return "", "", fmt.Errorf("credencial local não encontrada para o ID: %s", targetID)
}