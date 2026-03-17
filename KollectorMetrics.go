package main

import (
        "fmt"
        "regexp"

        "github.com/prometheus/client_golang/prometheus"
)

// metricas próprias do coletor
var KollectorMetrics []prometheus.Gauge // guarda os valores de metricas padroes do Kollector
var RegistredDbs int
var RegistredQueries int

var QueryWithErrors *prometheus.GaugeVec // guarda as querys com erros e suas informações

var FunctionDuration *prometheus.GaugeVec // guarda as duracoes de funções

func registerKollectorMetrics() {

        // metricas do próprio coletor
        // para criar uma nova metrica do coletor basta copiar um pedaço desse abaixo com append.

        // Inicializa a métrica GaugeVec para monitorar a conexão com as instâncias [0]
        FunctionDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
                Name: "KollectorFunctionDuration",
                Help: "Retorna o tempo de coleta de uma função",
        }, []string{"FunctionName"})

        reg.MustRegister(FunctionDuration)

        KollectorMetrics = append(KollectorMetrics, // insere a metrica no vetor de metricas do tipo '[]prometheus.Gauge'
                prometheus.NewGauge(prometheus.GaugeOpts{
                        Name: "KollectorOnlineSessions",
                        Help: "Retorna o numero de sessões online",
                }))

        KollectorMetrics = append(KollectorMetrics,
                prometheus.NewGauge(prometheus.GaugeOpts{
                        Name: "KollectorDisconnectedSessions",
                        Help: "Retorna o numero de sessões desconectadas",
                }))

        KollectorMetrics = append(KollectorMetrics,
                prometheus.NewGauge(prometheus.GaugeOpts{
                        Name: "KollectorRegistredDbs",
                        Help: "Retorna o numero de Bancos de dados registrados",
                }))

        KollectorMetrics = append(KollectorMetrics,
                prometheus.NewGauge(prometheus.GaugeOpts{
                        Name: "KollectorRegistredQueries",
                        Help: "Retorna o numero de Query's registradas",
                }))

        // adicionar novas metricas
        // KollectorMetrics = append(KollectorMetrics,
        //      prometheus.NewGauge(prometheus.GaugeOpts{
        //              Name: "NomeDaMetrica",
        //              Help: "DescricaoDaMetrica",
        //      }))
        // registrar todas as metricas que foram incluidas no vetor KollectorMetrics
        for _, kollmetric := range KollectorMetrics {
                reg.MustRegister(kollmetric)
        }

        QueryWithErrors = prometheus.NewGaugeVec(prometheus.GaugeOpts{
                Name: "KollectorQueryWithErrors",
                Help: "Retorna as querys com falha na execução",
        }, []string{"Area", "dbType", "db", "inst", "metric_family_name", "Error", "speed"})
        reg.MustRegister(QueryWithErrors)

}

func updateKollectorMetricsValues() {

        if timer_kollector_metrics >= KollAppConfigs.WaitKollectorMetricsUpdate {

                fmt.Print("[updateKollectorMetrics] Atualizando métricas padrão do Kollector...\n")

                // Iterar sobre cada métrica e definir seus valores
                for _, kollmetric := range KollectorMetrics {

                        desc := kollmetric.Desc().String() // salvar o conteudo completo da descrição da metrica, algo como: Desc{fqName: "KollectorOnlineSessions", help: "Return number of sessions online", constLabels: {}, variableLabels: {}}

                        // Verificar se a descrição corresponde à métrica de sessões online
                        if regexp.MustCompile(`KollectorOnlineSessions`).MatchString(desc) {
                                kollmetric.Set(float64(len(OnlineSessions))) // Definir o valor do número de sessões online
                        }

                        // Verificar se a descrição corresponde à métrica de sessões desconectadas
                        if regexp.MustCompile(`KollectorDisconnectedSessions`).MatchString(desc) {
                                kollmetric.Set(float64(len(DisconnectedSessions))) // Definir o valor do número de sessões desconectadas
                        }

                        // Verificar se a descrição corresponde à métrica de sessões desconectadas
                        if regexp.MustCompile(`KollectorRegistredDbs`).MatchString(desc) {
                                kollmetric.Set(float64(RegistredDbs)) // Definir o valor do número de sessões desconectadas
                        }

                        // Verificar se a descrição corresponde à métrica de sessões desconectadas
                        if regexp.MustCompile(`KollectorRegistredQueries`).MatchString(desc) {
                                kollmetric.Set(float64(RegistredQueries)) // Definir o valor do número de sessões desconectadas
                        }

                        // adicionar novos ifs para novas metricas
                        // if regexp.MustCompile(`NomeDaMetrica`).MatchString(desc) {
                        //      kollmetric.Set(float64(valor))
                        // }

                        timer_kollector_metrics = 0 // apos realizar todas as atualizações, resetar o contador de tempo
                }

        }

}
