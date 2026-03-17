package main

import (
        "database/sql"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"
        "unicode"

        "github.com/fatih/color"
        "gopkg.in/yaml.v3"
)

var red = color.New(color.FgRed).PrintfFunc()
var green = color.New(color.FgGreen).PrintfFunc()
var yellow = color.New(color.FgYellow).PrintfFunc()
var gray = color.New(color.FgHiBlack).PrintfFunc()
var blue = color.New(color.FgBlue).PrintfFunc()
var magenta = color.New(color.FgHiMagenta).PrintfFunc()
var cyan = color.New(color.FgCyan).PrintfFunc()

//var WaitTimers map[string]int = make(map[string]int)
//var RunningTimers map[string]int = make(map[string]int)
//var HaveTimers map[string]bool = make(map[string]bool)

func readSettings(filePath string) (AppConfigs, []RunConfigs, error) {

        var kollectorsonfigs KollectorConfigs

        yamlFile, err := os.ReadFile(filePath)
        if err != nil {
                red("[readSettings] Erro ao ler o arquivo YAML: %s\n", err)
                os.Exit(1)
        }

        err = yaml.Unmarshal(yamlFile, &kollectorsonfigs)
        if err != nil {
                red("[readSettings] Erro ao decodificar o arquivo YAML: %s\n", err)
                os.Exit(1)
        }

        return kollectorsonfigs.AppConfigs, kollectorsonfigs.RunConfigs, nil
}

func readYAML() ([]Database, error) {

        var yamlFiles []string // variavel que vai armazenar os caminhos dos arquivos .yml

        entries, err := os.ReadDir(KollAppConfigs.DatabasesDir)
        if err != nil {
                red("\n[readYAMl] Erro ao ler diretório: %s\n", err)
                os.Exit(1)
        }

        for _, entry := range entries {
                if entry.IsDir() {
                        continue
                }
                if filepath.Ext(entry.Name()) == ".yml" {
                        yamlFiles = append(yamlFiles, filepath.Join(KollAppConfigs.DatabasesDir, entry.Name())) // fazer append exemplo: config/sinc-oracle.yml
                }
        }

        if len(yamlFiles) == 0 {
                red("[readYAML] Nenhum arquivo YML encontrado no diretório %s.\n", KollAppConfigs.DatabasesDir)
                os.Exit(1)
        }

        var allDbs []Database

        fmt.Println("----> ymlFiles: ", yamlFiles)

        for _, path := range yamlFiles {

                fmt.Printf("[readYAML] importando dbs e configs do arquivo: %s\n", path)
                yamlFile, err := os.ReadFile(path)
                if err != nil {
                        red("[readYAML] Erro ao abrir o arquivo .yml: %s, %s\n", path, err)
                        return nil, err
                }

                //fmt.Println("[readYAML] decodificando .yml em uma estrutura 'DatabaseList'")
                var dbList DatabaseList
                err = yaml.Unmarshal(yamlFile, &dbList)
                if err != nil {
                        red("[readYAML] Erro ao decodificar o arquivo YAML: %s, %s\n", path, err)
                        return nil, err
                }

                // definindo arquivo local da configuração.
                for index := range dbList.Dbs {
                        db := &dbList.Dbs[index]
                        // substituindo barras invertidas por / normal
                        path = strings.Replace(path, "\\", "/", -1)
                        db.Arquivo = path
                }
                //

                // adicionar os commands atuais em AllCommands
                allDbs = append(allDbs, dbList.Dbs...)
        }

        // Imprimir JSON formatado
        fmt.Println("[readYAML] returning alldbs")
        // fmt.Println(string(jsonData))

        return allDbs, nil
}

func jsonLoader() Commands {

        var jsonFiles []string // variavel que vai armazenar os caminhos dos arquivos .json

        entries, err := os.ReadDir(KollAppConfigs.CommandsDir)
        if err != nil {
                red("\n[jsonLoader] Erro ao ler diretório: %s\n", err)
                os.Exit(1)
        }

        for _, entry := range entries {
                if entry.IsDir() {
                        continue
                }
                if filepath.Ext(entry.Name()) == ".json" {
                        jsonFiles = append(jsonFiles, filepath.Join(KollAppConfigs.CommandsDir, entry.Name())) // fazer append exemplo: commands/querys-Sinc.json
                }
        }

        if len(jsonFiles) == 0 {
                red("Nenhum arquivo JSON encontrado no diretório: %s.\n", KollAppConfigs.CommandsDir)
                os.Exit(1)
        }

        var AllCommands Commands

        fmt.Println("----> jsonFiles: ", jsonFiles)

        for _, path := range jsonFiles {

                fmt.Printf("[jsonLoader] importando querys e configs do arquivo %s\n", path)
                CommandsJson, err := os.Open(path)
                if err != nil {
                        red("[jsonLoader] Erro ao abrir o arquivo .json: %s, %s\n", path, err)
                }
                defer CommandsJson.Close()

                //fmt.Println("[jsonLoader] decodificando .json em uma estrutura 'Commands'")
                var commands Commands
                if err := json.NewDecoder(CommandsJson).Decode(&commands); err != nil {
                        red("[jsonLoader] Erro ao decodificar o JSON: %s, %s\n", path, err)
                        os.Exit(1)
                }

                // Definindo caminho do arquivo da query
                for j := range commands.Commands {
                        command := &commands.Commands[j]
                        // Trocando barras invertidas por barra normal /
                        path = strings.Replace(path, "\\", "/", -1)
                        command.Arquivo = path
                }
                //

                // adicionar os commands atuais em AllCommands
                AllCommands.Commands = append(AllCommands.Commands, commands.Commands...)
        }

        // Imprimir JSON formatado
        fmt.Println("[jsonLoader] returning AllCommands: ")
        //fmt.Println(string(jsonData))

        return AllCommands
}

func strClean(raw *sql.RawBytes) string {
        var numbers []rune

        // Iterar sobre cada byte do *sql.RawBytes
        for _, b := range *raw {
                // Verificar se o byte é um número
                if unicode.IsDigit(rune(b)) {
                        // Se for um número, adicioná-lo à slice de números
                        numbers = append(numbers, rune(b))
                }
        }

        // Retornar os números como uma string
        return string(numbers)
}

func dbConnector(dbType, Name, user, password, Port, ip string) (*sql.DB, error) {

        var connectionString string //variavel que armazena a string de conexão
        var driver string           //variavel que armazena o nome do driver referente ao banco

        fmt.Printf("[dbConnector] definindo o tipo do banco: '%s' | driver e a connectionString.\n", dbType)

        if len(Port) == 0 { // verificar se foi passada uma porta customizada no .yml se for 0 não, então pegar uma padrão
                Port = map[string]string{
                        "oracle":     "1521",
                        "mssql":      "1433",
                        "mssql_ad":   "1433",
                        "mssql_13":   "1433",
                        "postgresql": "5432",
                        "mysql":      "3306",
                }[dbType]
        }

        ConnectionStrings := map[string]string{
                "oracle":     "user=%s password=%s connectString=%s:%s/%s",
                "mssql":      "Driver={ODBC Driver 18 for SQL Server};Server=%s,%s;Database=%s;UID=%s;PWD=%s;",
                "mssql_ad":   "Driver={ODBC Driver 18 for SQL Server};Server=%s,%s;Database=%s;Authentication=ActiveDirectoryPassword;UID=%s;PWD=%s;",
                "mssql_13":   "Driver={ODBC Driver 13 for SQL Server};Server=%s,%s;Database=%s;Authentication=ActiveDirectoryPassword;UID=%s;PWD=%s;",
                "postgresql": "postgres://%s:%s@%s:%s/%s?sslmode=disable",
                "mysql":      "%s:%s@tcp(%s:%s)/%s?parseTime=true",
        }

        // connectionString := fmt.Sprintf("user=%s password=%s connectString=(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=%s)(PORT=%d))(CONNECT_DATA=(SERVICE_NAME=%s)))", user, password, ip, port, Name)

        switch dbType {
        case "oracle", "postgresql":
                connectionString = fmt.Sprintf(ConnectionStrings[dbType], user, password, ip, Port, Name)
        case "mssql", "mssql_ad", "mssql_13":
                connectionString = fmt.Sprintf(ConnectionStrings[dbType], ip, Port, Name, user, password)
        case "mysql":
                connectionString = fmt.Sprintf(ConnectionStrings[dbType], user, password, ip, Port, Name)
        default:
                connectionString = "default on switch case, erro na hora de verificar o tipo do banco de dados"
                driver = "default on switch case, erro na hora de verificar o tipo do banco de dados"
                red("[dbConnector] default on switch case, erro na hora de verificar o tipo do banco de dados %s", dbType)
        }

        driver = map[string]string{
                "oracle":     "godror",
                "mssql":      "odbc",
                "mssql_ad":   "odbc",
                "mssql_13":   "odbc",
                "postgresql": "pgx",
                "mysql":      "mysql",
        }[dbType]

        fmt.Println("[dbConnector] Realizando conexão no banco de dados "+dbType+" com connectionString: ", connectionString)

        // db, err := sql.Open(driver, connectionString)
        // if err != nil {
                // red("[dbConnector] Erro ao abrir a conexão: %s\n", err)
        // } else {
                // green("[dbConnector] Conexão via sql.Open() realizada com sucesso!\n")
        // }

        // return db, nil

        db, err := sql.Open(driver, connectionString)
        if err != nil {
            red("[dbConnector] Erro ao abrir a conexão: %s\n", err)
            return nil, err
        }
        green("[dbConnector] Conexão via sql.Open() realizada com sucesso!\n")

        // valida de verdade (muito recomendado)
        if err := db.Ping(); err != nil {
            red("[dbConnector] Ping falhou: %s\n", err)
            _ = db.Close()
            return nil, err
        }

        return db, nil

}

// definimos timer slow como o wait slow + 1 para já de cara realizar todas as querys e metricas
// var timer_slow int
// var timer_normal int
// var timer_fast int
var timer_disconnected_sessions int = 0
var timer_kollector_metrics int = 0

func resetTimeToDo() {

        for _, currentRunConfig := range KollRunConfigs {
                __MapTimers.mapTimersSet("RunningTimers", currentRunConfig.Name, *currentRunConfig.WaitSeconds+1) // subir o tempo para as queries já rodarem
                //__MapTimers.mapTimersSet("RunningTimers", currentRunConfig.Name, 0) // resetar tempo para 0
        }
        timer_disconnected_sessions = 0
        timer_kollector_metrics = KollAppConfigs.WaitKollectorMetricsUpdate + 1
}

var debounceTimer *time.Timer // variavel para garantir que o watcher abaixo (func FileListener) não chame as atualizações mais de uma vez por ocorrência.
var stopOnce sync.Once

func StopRoutines() {

        stopOnce.Do(func() {
                yellow("[StopRoutines] Realizando Stop nas goroutines...\n")
                close(stopChannel)
        })

        yellow("[StopRoutines] Aguardando goroutines finalizarem...\n")
        wg.Wait()

        green("\n\n[StopRoutines] ----------goroutines finalizadas----------\n\n")
}

func containsCommand(slice []Command, a Command) bool {
        for _, b := range slice {

                if a.DbType != b.DbType ||
                        a.Query != b.Query ||
                        a.Arquivo != b.Arquivo ||
                        a.MetricFamilyName != b.MetricFamilyName ||
                        a.MetricFamilyDesc != b.MetricFamilyDesc ||
                        a.Speed != b.Speed ||
                        a.OneInstQuery != b.OneInstQuery {
                        continue // Se encontrar alguma diferença, passa para o próximo comando
                }

                if len(a.Labels) != len(b.Labels) {
                        continue // Diferente número de labels, passa para o próximo comando
                }

                labelsMatch := true
                for i := range a.Labels {
                        if a.Labels[i] != b.Labels[i] {
                                labelsMatch = false
                                break
                        }
                }

                if labelsMatch {
                        return true // Encontrou uma correspondência
                }
        }

        return false // Não encontrou correspondência
}
