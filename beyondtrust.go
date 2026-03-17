package main

import (
        "bytes"
        "crypto/tls"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/cookiejar"
        "strconv"
        "sync"
        "time"
)

// -----------------------------
// BeyondTrust / Password Safe
// -----------------------------
// Seu tenant autentica via endpoint /Auth/SignAppin usando o header:
//   Authorization: PS-Auth key=<APIKEY>; runas=<USUARIO>;
// Esse POST cria uma sessão baseada em cookie (ASP.NET_SessionId) que deve
// ser mantida nas próximas chamadas.

// Estruturas de comunicação com a API BeyondTrust

type CredentialRequest struct {
        SystemName  string `json:"systemName"`
        AccountName string `json:"accountName"`
        Duration    int    `json:"durationMinutes"` // Duração do checkout em minutos
        Reason      string `json:"reason"`
}

type CredentialResponse struct {
        RequestID int `json:"requestId"`
}

type PasswordResponse struct {
        Password string `json:"password"`
}

// ----------------------------------------------------------------------
// HTTP client + sessão (cookie jar)
// ----------------------------------------------------------------------

var (
        btClientOnce sync.Once
        btClient     *http.Client
        btClientErr  error

        btAuthMu sync.Mutex
        btAuthed bool
)

func getBTClient() (*http.Client, error) {
        btClientOnce.Do(func() {
                jar, err := cookiejar.New(nil)
                if err != nil {
                        btClientErr = fmt.Errorf("erro ao criar cookie jar: %w", err)
                        return
                }

                btClient = &http.Client{
                        Timeout: 15 * time.Second,
                        Jar:     jar,
                        Transport: &http.Transport{
                                ForceAttemptHTTP2: false, // evita HTTP/2 (seu tenant reclama HTTP_1_1_REQUIRED)
                                TLSNextProto:      make(map[string]func(string, *tls.Conn) http.RoundTripper),
                                TLSClientConfig: &tls.Config{
                                        Renegotiation: tls.RenegotiateFreelyAsClient, // servidor envia HelloRequest
                                },
                        },
                }
        })

        if btClientErr != nil {
                return nil, btClientErr
        }
        return btClient, nil
}

func btAuthHeader(cfg BeyondTrustConfig) string {
        // Mantém exatamente o formato do curl que funcionou.
        return fmt.Sprintf("PS-Auth key=%s; runas=%s;", cfg.APIKey, cfg.RunAs)
}

// ----------------------------------------------------------------------
// Auth: SignAppin (cookie-based)
// ----------------------------------------------------------------------

func signAppin(cfg BeyondTrustConfig) error {
        client, err := getBTClient()
        if err != nil {
                return err
        }

        authURL := cfg.BaseURL + "/Auth/SignAppin"
        // curl usou -d '' (body vazio) -> application/x-www-form-urlencoded.
        req, err := http.NewRequest("POST", authURL, bytes.NewBufferString(""))
        if err != nil {
                return fmt.Errorf("erro ao criar requisição SignAppin: %w", err)
        }

        req.Header.Set("Accept", "application/json")
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        req.Header.Set("Authorization", btAuthHeader(cfg))

        resp, err := client.Do(req)
        if err != nil {
                return fmt.Errorf("erro ao executar SignAppin: %w", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                bodyBytes, _ := io.ReadAll(resp.Body)
                return fmt.Errorf("falha no SignAppin, status: %d, resposta: %s", resp.StatusCode, string(bodyBytes))
        }

        return nil
}

func ensureSignAppin(cfg BeyondTrustConfig) error {
        btAuthMu.Lock()
        defer btAuthMu.Unlock()

        if btAuthed {
                return nil
        }
        if err := signAppin(cfg); err != nil {
                return err
        }
        btAuthed = true
        return nil
}

func btForceReauth() {
        btAuthMu.Lock()
        btAuthed = false
        btAuthMu.Unlock()
}

// ----------------------------------------------------------------------
// CHECKOUT / PASSWORD / CHECKIN
// ----------------------------------------------------------------------

func checkoutPassword(cfg BeyondTrustConfig, systemName, accountName string) (string, int, error) {
        if err := ensureSignAppin(cfg); err != nil {
                return "", 0, fmt.Errorf("falha ao autenticar no BeyondTrust (SignAppin): %w", err)
        }

        client, err := getBTClient()
        if err != nil {
                return "", 0, err
        }

        requestURL := cfg.BaseURL + "/Credentials/Requests"
        checkoutData := CredentialRequest{
                SystemName:  systemName,
                AccountName: accountName,
                Duration:    5,
                Reason:      "Coleta de métricas Oracle pelo coletor de observabilidade",
        }

        jsonData, err := json.Marshal(checkoutData)
        if err != nil {
                return "", 0, fmt.Errorf("erro ao serializar checkoutData: %w", err)
        }

        req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
        if err != nil {
                return "", 0, fmt.Errorf("erro ao criar req de checkout: %w", err)
        }

        req.Header.Set("Accept", "application/json")
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", btAuthHeader(cfg))

        resp, err := client.Do(req)
        if err != nil {
                return "", 0, fmt.Errorf("erro ao executar requisição de checkout: %w", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode == http.StatusUnauthorized {
                // Cookie expirou / sessão inválida. Tenta reautenticar 1x.
                btForceReauth()
                if err := ensureSignAppin(cfg); err == nil {
                        return checkoutPassword(cfg, systemName, accountName)
                }
        }

        if resp.StatusCode != http.StatusCreated {
                bodyBytes, _ := io.ReadAll(resp.Body)
                return "", 0, fmt.Errorf("falha ao solicitar checkout, status: %d, resposta: %s", resp.StatusCode, string(bodyBytes))
        }

        var reqResp CredentialResponse
        if err := json.NewDecoder(resp.Body).Decode(&reqResp); err != nil {
                return "", 0, fmt.Errorf("erro ao decodificar resposta do checkout: %w", err)
        }

        // Obtém senha
        passwordURL := cfg.BaseURL + "/Credentials/" + strconv.Itoa(reqResp.RequestID) + "/Password"
        getReq, err := http.NewRequest("GET", passwordURL, nil)
        if err != nil {
                _ = checkinPassword(cfg, reqResp.RequestID)
                return "", reqResp.RequestID, fmt.Errorf("erro ao criar req de obtenção de senha: %w", err)
        }
        getReq.Header.Set("Accept", "application/json")
        getReq.Header.Set("Authorization", btAuthHeader(cfg))

        getResp, err := client.Do(getReq)
        if err != nil {
                _ = checkinPassword(cfg, reqResp.RequestID)
                return "", reqResp.RequestID, fmt.Errorf("erro ao executar requisição para obter senha: %w", err)
        }
        defer getResp.Body.Close()

        if getResp.StatusCode != http.StatusOK {
                bodyBytes, _ := io.ReadAll(getResp.Body)
                _ = checkinPassword(cfg, reqResp.RequestID)
                return "", reqResp.RequestID, fmt.Errorf("falha ao obter senha, status: %d, resposta: %s", getResp.StatusCode, string(bodyBytes))
        }

        var passResp PasswordResponse
        if err := json.NewDecoder(getResp.Body).Decode(&passResp); err != nil {
                _ = checkinPassword(cfg, reqResp.RequestID)
                return "", reqResp.RequestID, fmt.Errorf("erro ao decodificar senha: %w", err)
        }

        return passResp.Password, reqResp.RequestID, nil
}

func checkinPassword(cfg BeyondTrustConfig, requestID int) error {
        client, err := getBTClient()
        if err != nil {
                return err
        }

        checkinURL := cfg.BaseURL + "/Credentials/Requests/" + strconv.Itoa(requestID) + "/Checkin"
        req, err := http.NewRequest("POST", checkinURL, nil)
        if err != nil {
                return fmt.Errorf("erro ao criar req de check-in: %w", err)
        }

        req.Header.Set("Accept", "application/json")
        req.Header.Set("Authorization", btAuthHeader(cfg))
        req.Header.Set("Content-Length", "0")

        resp, err := client.Do(req)
        if err != nil {
                return fmt.Errorf("erro ao executar check-in: %w", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusNoContent {
                bodyBytes, _ := io.ReadAll(resp.Body)
                return fmt.Errorf("falha no check-in, status code: %d, resposta: %s", resp.StatusCode, string(bodyBytes))
        }

        return nil
}
