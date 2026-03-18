# --- ESTÁGIO 1: COMPILAÇÃO ---
FROM golang:1.23-alpine AS builder

# Instala pacotes necessários para baixar dependências e compilar drivers C (se necessário)
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Copia os arquivos de definição de módulos primeiro
COPY go.mod go.sum ./

# AJUSTE: Forçamos o download e a limpeza das dependências dentro do ambiente de build
# Isso evita o erro "updates to go.mod needed" durante o build final.
RUN go mod download && go mod tidy

# Copia todo o código fonte e a estrutura de pastas
COPY . .

# Compila o binário. 
# CGO_ENABLED=0 garante um binário estático que roda em qualquer Linux.
# -ldflags="-w -s" remove informações de debug para diminuir o tamanho.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o dbcollector ./cmd/main.go

# --- ESTÁGIO 2: EXECUÇÃO ---
FROM alpine:3.18

# Adiciona certificados CA (Obrigatório para o BeyondTrust via HTTPS)
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copia o binário do estágio anterior
COPY --from=builder /app/dbcollector .

# Copia a estrutura de pastas de configuração para dentro do container
COPY config/ ./config/

# Cria um usuário sem privilégios (Boa prática de segurança Sênior)
RUN adduser -D exporter
USER exporter

# Porta configurada no global.yaml
EXPOSE 42000

# Comando para iniciar o coletor
ENTRYPOINT ["./dbcollector"]