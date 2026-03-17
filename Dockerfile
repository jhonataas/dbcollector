# -------- build stage --------
FROM golang:1.22.2 AS build

WORKDIR /src

# deps nativas necessárias para build com CGO
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    gcc g++ \
    unixodbc-dev \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -o /out/kollector .

# -------- runtime stage --------
FROM debian:12-slim

# libs + drivers
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget unzip curl gnupg ca-certificates \
    libaio1 unixodbc \
    && rm -rf /var/lib/apt/lists/*

###################################################
################# Install SQLPLUS #################
###################################################
WORKDIR /montd/client

RUN wget -q https://download.oracle.com/otn_software/linux/instantclient/211000/instantclient-basic-linux.x64-21.1.0.0.0.zip && \
    wget -q https://download.oracle.com/otn_software/linux/instantclient/211000/instantclient-sqlplus-linux.x64-21.1.0.0.0.zip && \
    unzip -q instantclient-basic-linux.x64-21.1.0.0.0.zip -d /montd/client && \
    unzip -q instantclient-sqlplus-linux.x64-21.1.0.0.0.zip -d /montd/client && \
    mv instantclient_21_1/* /montd/client && \
    rm -f instantclient-basic-linux.x64-21.1.0.0.0.zip && \
    rm -f instantclient-sqlplus-linux.x64-21.1.0.0.0.zip && \
    rmdir instantclient_21_1

ENV LD_LIBRARY_PATH=/montd/client/:$LD_LIBRARY_PATH
ENV PATH=/montd/client/:$PATH
ENV TNS_ADMIN=/montd/client/network

###################################################
############ Install SQL Server Driver ############
###################################################

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl gnupg ca-certificates apt-transport-https && \
    rm -rf /var/lib/apt/lists/*

# Importa a chave para um keyring (modo recomendado)
RUN mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
      | gpg --dearmor -o /etc/apt/keyrings/microsoft.gpg && \
    chmod 644 /etc/apt/keyrings/microsoft.gpg

# Adiciona repositório usando signed-by
RUN echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/microsoft.gpg] https://packages.microsoft.com/debian/12/prod bookworm main" \
    > /etc/apt/sources.list.d/mssql-release.list

# Instala driver
RUN apt-get update && \
    ACCEPT_EULA=Y apt-get install -y --no-install-recommends msodbcsql18 && \
    rm -rf /var/lib/apt/lists/*


###################################################
##################### App #########################
###################################################
WORKDIR /app

# binário
COPY --from=build /out/kollector /app/kollector

# configs default (ainda pode sobrescrever com volumes no docker-compose)
COPY KollectorSettings.yml /app/KollectorSettings.yml
COPY commands/ /app/commands/
COPY config/ /app/config/

# user não-root
RUN useradd -r -u 10001 -g root kollector && \
    chown -R kollector:root /app
USER 10001

EXPOSE 42000
CMD ["/app/kollector"]
