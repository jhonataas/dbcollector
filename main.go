package main

import (
        "context"
        "database/sql"
        "fmt"
        "log"
        "net/http"
        "os"
        "os/signal"
        "regexp"
        "strconv"
        "strings"
        "sync"
        "syscall"
        "time"

        _ "github.com/alexbrainman/odbc" //Importar o driver do SQL Server (mssql)
        "github.com/fatih/color"
        _ "github.com/go-sql-driver/mysql" // Importar o driver do MySQL
        _ "github.com/godror/godror"       // Importar o driver da Oracle
        _ "github.com/jackc/pgx/v5/stdlib" // Importar o driver do PostgreSQL
        "github.com/prometheus/client_golang/prometheus"
        "github.com/prometheus/client_golang/prometheus/promhttp"
)

type Database struct {
        Name      string   `yaml:"name"`
        Area      string   `yaml:"Area"`
        Arquivo   string   `yaml:"arquivo"`
        User      string   `yaml:"user"`
        BtSysName string   `yaml:"btsys_name"`
        BtAccName string   `yaml:"btacc_name"`
        DbType    string   `yaml:"dbType"`
        Port      string   `yaml:"port"`
        Ips       []string `yaml:"targets"`
        InstName  string   ""
}

// Configuração global para o BeyondTrust
type BeyondTrustConfig struct {
        BaseURL string
        APIKey string
        RunAs  string
}

type DatabaseList struct {
        Dbs []Database `yaml:"dbs"`
}

// AppConfigs representa a configuração do aplicativo
type AppConfigs struct {
        DatabasesDir                      string `yaml:"databases_dir"`
        CommandsDir                       string `yaml:"commands_dir"`
        WaitKollectorMetricsUpdate        int    `yaml:"wait_kollector_metrics_update"`
        waitDisconnectedSessionsReconnect int    `yaml:"wait_disconnected_sessions_reconnect"`
}

// RunConfig representa uma configuração de execução específica
type RunConfigs struct {
        Name            string   `yaml:"name"`
        WaitSeconds     *int     `yaml:"wait_seconds,omitempty"`
        OnlyRunOnDbType []string `yaml:"only_run_on_dbtype,omitempty"`
        OnlyRunOnArea   []string `yaml:"only_run_on_area,omitempty"`
        OnlyRunOnDb     []string `yaml:"only_run_on_db,omitempty"`
}

// Config representa a configuração geral
type KollectorConfigs struct {
        AppConfigs AppConfigs   `yaml:"app_configs"`
        RunConfigs []RunConfigs `yaml:"run_configs"`
}

type Command struct {
        DbType           string `json:"dbType"`
        Query            string `json:"query"`
        Arquivo          string `json:"arquivo,omitempty"`
        MetricFamilyName string `json:"metricFamilyName"`
        MetricFamilyDesc string `json:"metricFamilyDesc"`
        Speed            string `json:"speed"`
        OneInstQuery     bool   `json:"oneInstQuery"`
        //ExpectedLabels   int
        Labels        []string
        CounterFamily *prometheus.GaugeVec
}

type Commands struct {
        Commands []Command `json:"Commands"`
}

type CollectedMetrics struct {
        sync.Mutex
        MetricsAndLabels map[string][]string
}

var BTConfig BeyondTrustConfig                          // Variável global para as configs do cofre
var CtxTimeOut int = 2                                  // time-out do tempo limite para conectar em uma instance.
var reg *prometheus.Registry = prometheus.NewRegistry() // variavel global que cria o registro de metricas
var OnlineSessions = make(map[*sql.DB]Database)         // guarda todas as sessões (ips) online
var DisconnectedSessions = make(map[*sql.DB]Database)   // guarda todas as sessões (ips) offline
var commands Commands                                   // Variável global para armazenar os comandos
var Databases []Database                                // Variavel global para armazenar os dbs
var consoleLogs []string                                // Variável global para armazenar os logs
var counterFamilyCommands *prometheus.GaugeVec          // variavel que armazena o tipo de metrica para os commands
var AllMetricsFamilyUp *prometheus.GaugeVec             // variavel global para armazenar metrica DbInstUp
var KollAppConfigs AppConfigs
var KollRunConfigs []RunConfigs

func getSQLQueryLabels(Query string) []string {

        // Palavras-chave que não devem ser consideradas como aliases
        keywords := map[string]struct{}{
                "DATE": {}, "TIME": {}, "TIMESTAMP": {}, "DATETIME": {}, "date": {}, "time": {}, "timestamp": {}, "datetime": {},
        }

        //
        queryParts := strings.Split(Query, "FROM") // dividir a query em 2 pois não queremos AS depois disso
        rgx := regexp.MustCompile(`\b(?:as|AS)\b\s([\p{L}\p{N}_]+)`)
        labelMatches := rgx.FindAllStringSubmatch(queryParts[0], -1)

        fmt.Println(`[registerMetrics] Coletando labels da query: `, queryParts[0])

        var queryLabels = []string{"dbType", "db", "inst", "Area"} // incluir 4 labels obrigatórias

        // index 0 é toda a ocorrência da regex, 1,2,3... são as colunas
        for _, label := range labelMatches {

                labelToInsert := label[1]

                if _, isKeyWord := keywords[labelToInsert]; !isKeyWord {
                        fmt.Println(`[registerMetrics] label encontrada: `, label[1])
                        queryLabels = append(queryLabels, label[1])
                }

        }
        //

        return queryLabels
}

func registerOneQueryMetric(command *Command, queryLabels []string, reg prometheus.Registerer) {

        // Inicializa a métrica GaugeVec para as métricas e salva em um map
        counterFamilyCommands = prometheus.NewGaugeVec(prometheus.GaugeOpts{
                Name: command.MetricFamilyName,
                Help: command.MetricFamilyDesc,
        }, queryLabels)

        reg.MustRegister(counterFamilyCommands)

        //command.ExpectedLabels = len(queryLabels) // quantas labels o command deve ter

        command.CounterFamily = counterFamilyCommands
        command.Labels = queryLabels
        //AllQueryMetricsFamily[counterFamilyCommands] = append([]string{command.MetricFamilyName}, queryLabels...) // adicionando nome da metrica + labels
}

func registerMetrics(folderState bool) {

        fmt.Println("[registerMetrics] registrando métricas no 'reg'")

        if folderState {
                yellow("[unreg] Removendo registro de metricas para dbInstUp\n")
                reg.Unregister(AllMetricsFamilyUp)
                // Iterando sobre o mapa para desregistrar
                for _, command := range commands.Commands {
                        yellow("[unreg] Removendo registro de metricas para commands\n")
                        reg.Unregister(command.CounterFamily)
                }
                //
        }

        // Inicializa a métrica GaugeVec para monitorar a conexão com as instâncias [0]
        AllMetricsFamilyUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
                Name: "DbInstUP",
                Help: "Return connection with instance of db",
        }, []string{"dbType", "db", "inst", "Area"})

        reg.MustRegister(AllMetricsFamilyUp)

        //------------------

        fmt.Println("[registerMetrics] registrando métrica 'counterFamily' no map AllQueryMetricsFamily")

        RegistredQueries = len(commands.Commands) // contar quantas query's foram registradas

        for i := range commands.Commands { // iterar sobre as querys do arquivo .json

                command := &commands.Commands[i] // obtém um ponteiro para o elemento atual

                red("########>> [ %s ] for index: %d\n", command.MetricFamilyName, i)

                // setar que o timer existe
                __MapTimers.mapTimersSet("HaveTimers", command.Speed, true)

                queryLabels := getSQLQueryLabels(command.Query)
                registerOneQueryMetric(command, queryLabels, reg)
        }
}

func loginOneDb(db *Database, ip string) *sql.DB {
        // 1) Checkout da senha via BeyondTrust (Auth/SignAppin + cookie de sessão)
        password, requestID, err := checkoutPassword(BTConfig, db.BtSysName, db.BtAccName)
        if err != nil {
                red("[loginOneDb] Falha ao obter senha BT para %s: %s\n", db.Name, err.Error())
                return nil
        }

        // Garante o checkin ao final (mesmo se falhar o login no DB)
        defer func() {
                if err := checkinPassword(BTConfig, requestID); err != nil {
                        red("[loginOneDb] Falha ao fazer check-in BT (requestId=%d) para %s: %s\n", requestID, db.Name, err.Error())
                }
        }()

        // 2) Conecta ao DB com a senha recuperada
        // dbConn := getDBConn(db, ip, password)
        // if dbConn == nil {
        //      return nil
        // }

        dbConn, err := dbConnector(db.DbType, db.Name, db.User, password, db.Port, ip)
        if err != nil || dbConn == nil {
            red("[loginOneDb] Falha ao conectar no DB %s: %v\n", db.Name, err)
            return nil
        }
        // 3) Carrega versão/metadata do banco
        // initDBVersion() existia em versões anteriores do projeto para coletar metadados
        // do banco. Não é necessário para o fluxo atual do coletor.

        return dbConn
}


func loginAllDbs(dbs []Database) map[*sql.DB]Database {
        Sessions := make(map[*sql.DB]Database)

        for _, db := range dbs {
                for index, ip := range db.Ips {
                        fmt.Printf("[loginAllDbs] Realizando login no banco: %s (ip: %s)\n", db.Name, ip)

                        actualSession := loginOneDb(&db, ip)
                        if actualSession == nil {
                                yellow("[loginAllDbs] Sessão nil para db: %s ip: %s (pulando)\n", db.Name, ip)
                                continue
                        }

                        instDb := Database{
                                Name:      db.Name,
                                Area:      db.Area,
                                Arquivo:   db.Arquivo,
                                User:      db.User,
                                BtSysName: db.BtSysName,
                                BtAccName: db.BtAccName,
                                Port:      db.Port,
                                DbType:    db.DbType,
                                Ips:       []string{ip}, // guarda apenas o target desta instância
                                InstName:  db.Name + strconv.Itoa(index+1),
                        }

                        Sessions[actualSession] = instDb
                }
        }

        return Sessions
}

func tryToUpDisconnectedSessions(Sessions map[*sql.DB]Database) {

        if timer_disconnected_sessions >= KollAppConfigs.waitDisconnectedSessionsReconnect {

                for session, database := range Sessions {

                        if testDbConnection(session, database) {
                                green("[runSelfMonitoringRoutines] ip: %s está Up! setando counterFamilyUp como 1 e removendo do vetor DisconnectedSessions\n", database.InstName)
                                AllMetricsFamilyUp.WithLabelValues(database.DbType, database.Name, database.InstName, database.Area).Set(1)
                                delete(DisconnectedSessions, session) // removendo a sessão da lista de sessões desconectadas
                                OnlineSessions[session] = database    // adicionando a sessão a lista de sessões online
                        } else {
                                yellow("[runSelfMonitoringRoutines] ip: %s ainda está Down! mantendo no vetor DisconnectedSessions\n", database.InstName)
                                time.Sleep(1 * time.Second)
                        }

                }

                timer_disconnected_sessions = 0

        } else {
                time.Sleep(500 * time.Millisecond)
        }
}

func runSelfMonitoringRoutines(routineName string, SelfData interface{}, done <-chan struct{}, wg *sync.WaitGroup) {

        defer wg.Done() // Garante que wg.Done() seja chamado antes de retornar da função

        for {

                select {
                case <-done:
                        fmt.Printf("[runSelfMonitoringRoutines] Goroutine ended -> runSelfMonitoringRoutines()\n")
                        return
                default:

                        switch routineName {
                        case "tryToUpDisconnectedSessions":
                                tryToUpDisconnectedSessions(SelfData.(map[*sql.DB]Database))
                        case "updateKollectorMetricsValues":
                                updateKollectorMetricsValues()
                        default:
                                yellow("[runSelfMonitoringRoutines] Erro na função runSelfMonitoringRoutines(): switch não encontrou a routineName %s\n", routineName)
                                return
                        }

                }
        }

}

func testDbConnection(session *sql.DB, database Database) bool {
        // fmt.Println("[testDbConnection] criando novo contexto ctx de timeout para verificação de instancia")
        ctx, cancel := context.WithTimeout(context.Background(), time.Duration(CtxTimeOut)*time.Second)
        defer cancel()

        var testQuery string
        if database.DbType == "oracle" {
                testQuery = "SELECT 1 FROM DUAL"
        } else {
                testQuery = "SELECT 1"
        }

        var dummy int
        err := session.QueryRowContext(ctx, testQuery).Scan(&dummy)

        if err != nil {
                fmt.Printf("[testDbConnection] Falha ao testar conexão: %v\n", err)
                return false
        }

        return true
}

func prepareToInsertQueryMetric(rowValues []interface{}, LabelValues []string) ([]string, []float64) {

        var Values []float64
        var floatStrValue float64 = -1.0

        // fmt.Println("[prepareToInsertQueryMetric] Iterando sobre rowValues | len(rowValues): " + strconv.Itoa(len(rowValues)))
        for i, value := range rowValues {

                //fmt.Println("[prepareToInsertQueryMetric] convertendo valor para string e salvando em 'strValue'") // Verificar se o valor é do tipo *sql.RawBytes
                strValue, ok := value.(*sql.RawBytes)
                //fmt.Println("[prepareToInsertQueryMetric] salvando valor limpo em 'cleanedStrValue'")
                cleanedStrValue := strClean(strValue)
                if !ok {
                        //addQueryMetricError("[prepareToInsertQueryMetric] Erro ao converter valor para *sql.RawBytes" + "\n")
                        continue
                }
                // Exibir o índice e o valor convertido para string
                //fmt.Printf("[prepareToInsertQueryMetric] i: %d | cleanedStrValue: %s | *strValue: %s | len(rowValues): %d\n", i, cleanedStrValue, *strValue, len(rowValues))

                if len(rowValues) > 1 && i < len(rowValues)-1 {
                        //fmt.Println("[prepareToInsertQueryMetric] fazendo append em LabelValues, | i: "+strconv.Itoa(i)+" len(rowValues)-1: "+strconv.Itoa(len(rowValues)-1)+" string(*strValue): ", string(*strValue))
                        var stringToAppend string = string(*strValue)
                        if len(stringToAppend) == 0 {
                                stringToAppend = "null"
                        }
                        LabelValues = append(LabelValues, stringToAppend)
                } else {
                        var err error
                        floatStrValue, err = strconv.ParseFloat(cleanedStrValue, 64)
                        if err != nil {
                                yellow("Erro ao tentar converter *strValue: " + string(*strValue) + " para float64, erro: " + err.Error() + "\n")
                        }
                        //fmt.Println("[prepareToInsertQueryMetric] on else inserindo um valor único em Values: ", floatStrValue)
                        Values = append(Values, floatStrValue)
                }

        }

        return LabelValues, Values
}

func executeQuery(session *sql.DB, database *Database, command *Command) (*sql.Rows, []interface{}) {

        //fmt.Println("[executeQuery] Realizando query: " + command.Query + "\n")
        rows, err := session.Query(command.Query)
        if err != nil {
                //red("[executeQuery] Erro ao executar a query: %s, erro: %s\n", Query, err)
                addQueryMetricError("Erro ao executar a query: "+err.Error()+"\n", *database, *command)
                return nil, nil
        }

        // fmt.Println("[executeQuery] obtendo os nomes das colunas")
        columns, err := rows.Columns()
        if err != nil {
                red("[executeQuery] Erro ao obter os nomes das colunas: " + err.Error() + "\n")
                addQueryMetricError("Erro ao obter os nomes das colunas: "+err.Error()+"\n", *database, *command)
                return nil, nil
        }

        // fmt.Printf("[executeQuery] criando um slice de strings para armazenar os valores das columns: %s\n", columns)
        values := make([]interface{}, len(columns))
        for i := range values {
                values[i] = new(sql.RawBytes)
        }

        return rows, values
}

func addQueryMetricError(ErrorifExits string, database Database, command Command) {
        //red("[addQueryMetricError] %s\n", ErrorifExits)
        QueryWithErrors.WithLabelValues([]string{database.Area, command.DbType, database.Name, database.InstName, command.MetricFamilyName, ErrorifExits, command.Speed}...).Set(0)
}

var wg sync.WaitGroup
var stopChannel = make(chan struct{})

func OrganizeSessions() {

        magenta("[OrganizeSessions] >>> 1.0\n")

        // contagem de threads abertas
        var threadsOpenCount int = 0

        // Goroutine que vai cuidar das sessões desconectadas caso existam
        wg.Add(1)
        go runSelfMonitoringRoutines("tryToUpDisconnectedSessions", DisconnectedSessions, stopChannel, &wg)
        threadsOpenCount++

        // Goroutine que vai atualizar os dados das métricas do coletor (Self-Monitoring)
        wg.Add(1)
        go runSelfMonitoringRoutines("updateKollectorMetricsValues", struct{}{}, stopChannel, &wg)
        threadsOpenCount++

        var verifyRunOnCondition = func(comparisonItem string, OnlyRunOnThisItems []string) bool {
                for _, Item := range OnlyRunOnThisItems {
                        if comparisonItem == Item || Item == "any" {
                                return true
                        }
                }
                return false
        }

        for _, runCfgs := range KollRunConfigs {

                var OrganizedSessions []map[*sql.DB]Database
                var OrganizedCommands []Command

                for session, database := range OnlineSessions {

                        //yellow("verifying: a: %b || b: %b || c: %b\n", verifyIfRunOnDbType(database.DbType, runCfgs.OnlyRunOnDbType), verifyIfRunOnArea(database.Area, runCfgs.OnlyRunOnArea), verifyIfRunOnDb(database.Name, runCfgs.OnlyRunOnDb))
                        //gray("database.DbType: %s || database.Area: %s || database.Name: %s\n", database.DbType, database.Area, database.Name)

                        if verifyRunOnCondition(database.DbType, runCfgs.OnlyRunOnDbType) && verifyRunOnCondition(database.Area, runCfgs.OnlyRunOnArea) && verifyRunOnCondition(database.Name, runCfgs.OnlyRunOnDb) {

                                OrgSession := map[*sql.DB]Database{session: database}     // iniciando 1 mapa e já inserindo valor nele.
                                OrganizedSessions = append(OrganizedSessions, OrgSession) // inserindo o item criado no vetor de sessões organizadas

                                for _, currentCommand := range commands.Commands { // iterando sobre todas as queries
                                        if currentCommand.Speed == runCfgs.Name && currentCommand.DbType == database.DbType && !containsCommand(OrganizedCommands, currentCommand) {
                                                OrganizedCommands = append(OrganizedCommands, currentCommand)
                                        }
                                }
                        }

                }

                if len(OrganizedCommands) > 0 { // se tiver alguma sessão e alguma query

                        OrganizedSession := map[string]interface{}{
                                "dbType":   OrganizedCommands[0].DbType, // pegar o dbType do primeiro item já que será o mesmo em todos
                                "Speed":    runCfgs.Name,
                                "Sessions": OrganizedSessions,
                                "Commands": OrganizedCommands,
                        }
                        //
                        wg.Add(1)
                        //

                        yellow("[doTasks] >>> Iniciando nova Goroutine para o speed: %s | dbType: %s\n", OrganizedSession["Speed"], OrganizedSession["dbType"])
                        fmt.Printf("[doTasks] >>> \tDatabases:\n")
                        for _, csession := range OrganizedSession["Sessions"].([]map[*sql.DB]Database) {
                                for _, db := range csession {
                                        yellow("[doTasks] >>> \tName: %s\n", color.CyanString(db.Name))
                                }
                        }

                        fmt.Printf("[doTasks] >>> \tCommands:\n")
                        for _, command := range OrganizedSession["Commands"].([]Command) {
                                yellow("[doTasks] >>> \tmetricFamilyName: %s\n", color.CyanString(command.MetricFamilyName))
                        }

                        go doTasks(OrganizedSession, stopChannel, &wg)
                        threadsOpenCount++
                }

        }

        cyan("\n[OrganizeSessions] %s %s\n", color.YellowString(strconv.Itoa(threadsOpenCount)), color.CyanString("Threads abertas no total\n"))
}

func checkOneInstQueryCondition(commandOneInstQuery bool, dbInstName string) bool {
        if commandOneInstQuery && strings.Contains(dbInstName, "1") || !commandOneInstQuery {
                return true
        }
        return false
}

func checkConditionsToQuery(SessionConnection *sql.DB, SessionDatabase Database, AllSpeedCommands []Command) {

        for _, inActionCommand := range AllSpeedCommands {

                if !checkOneInstQueryCondition(inActionCommand.OneInstQuery, SessionDatabase.InstName) {
                        yellow("[checkConditionsToQuery] Condição -> OneInstQuery não passou no teste, pulando query: %s...\n", color.CyanString(inActionCommand.MetricFamilyName))
                        continue
                }

                green("[checkConditionsToQuery] Executando Query: dbType: <%s> db:<%s> speed:<%s> %s...\n", color.CyanString(SessionDatabase.DbType), color.CyanString(SessionDatabase.Name), color.CyanString(inActionCommand.Speed), color.CyanString(inActionCommand.MetricFamilyName))
                rows, values := executeQuery(SessionConnection, &SessionDatabase, &inActionCommand)
                if rows == nil {
                        red("[checkConditionsToQuery] Nenhum resultado retornado na métrica: %s, db: %s, dbType: %s Pulando para próxima...\n", inActionCommand.MetricFamilyName, SessionDatabase.Name, inActionCommand.DbType)
                        //addQueryMetricError("Nenhum resultado retornado ou houve um erro durante a consulta.", SessionDatabase, inActionCommand)
                        inActionCommand.OneInstQuery = false // se tivermos erro ao executar a query em Inst1 desative a opção para habilitar a Inst2
                        continue
                }
                defer rows.Close()
                // Processe os valores aqui, por exemplo, chamando a função printRow para imprimir cada linha

                for rows.Next() {

                        // fmt.Println("\n[checkConditionsToQuery] criando var rowValues e fazendo append de values...")
                        var rowValues []interface{}
                        rowValues = append(rowValues, values...)

                        //fmt.Println("[checkConditionsToQuery] escaneando rows com Scan(rowValues...)")
                        err := rows.Scan(rowValues...)
                        if err != nil {
                                addQueryMetricError("[checkConditionsToQuery] Erro ao escanear os valores das colunas: "+err.Error()+"\n", SessionDatabase, inActionCommand)
                                continue
                        }

                        var LabelValues []string
                        var Values []float64
                        LabelValues = append(LabelValues, SessionDatabase.DbType, SessionDatabase.Name, SessionDatabase.InstName, SessionDatabase.Area)
                        LabelValues, Values = prepareToInsertQueryMetric(rowValues, LabelValues)

                        if len(LabelValues) != len(inActionCommand.Labels) {
                                //addQueryMetricError("[checkConditionsToQuery] Quantidade de labels incorreta na metrica: "+inActionCommand.MetricFamilyName+" Esperadas: "+strconv.Itoa(inActionCommand.ExpectedLabels)+" Recebidas: "+strconv.Itoa(len(LabelValues)), SessionDatabase, inActionCommand)
                                red("\n\n[checkConditionsToQuery] Quantidade de labels incorreta na metrica: %s Esperadas: %d - Recebidas: %d\n\n", inActionCommand.MetricFamilyName, len(inActionCommand.Labels), len(LabelValues))
                                os.Exit(1)
                        }

                        for _, value := range Values {
                                inActionCommand.CounterFamily.WithLabelValues(LabelValues...).Set(value)
                                //fmt.Printf("[checkConditionsToQuery] Métrica: <%s> frequency: <%s> labels: %s inserida com sucesso!\n", color.BlueString(inActionCommand.MetricFamilyName), color.BlueString(inActionCommand.Speed), LabelValues)
                        }

                }
        }
}

func checkIfTimeToDo(speed string) bool {

        var currentRunningTimerInt interface{} = __MapTimers.mapTimersGet("RunningTimers", speed)
        var currentWaitTimerInt interface{} = __MapTimers.mapTimersGet("WaitTimers", speed)

        //magenta("Verificando -> speed: %s runningExists: %+v || waitExists: %+v ---------> currentRunningTimerInt: %d || currentWaitInt: %d\n", speed, runningExists, waitExists, currentRunningTimerInt, currentWaitInt)

        if currentRunningTimerInt != nil && currentWaitTimerInt != nil {
                return currentRunningTimerInt.(int) >= currentWaitTimerInt.(int)
        }

        return false
}

func ResetMetrics(CommandsToReset *[]Command) {
        for _, command := range *CommandsToReset {
                command.CounterFamily.Reset()
        }
}

func doTasks(
        OneSession map[string]interface{},
        done <-chan struct{},
        wg *sync.WaitGroup,
) {
        defer wg.Done()

        IncomingSessions := OneSession["Sessions"].([]map[*sql.DB]Database)
        IncomingCommands := OneSession["Commands"].([]Command)
        speed := OneSession["Speed"].(string)
        dbType := OneSession["dbType"].(string)

        for {
                select {
                case <-done:
                        fmt.Printf("[doTasks] Goroutine ended | dbType: %s | speed: %s\n", dbType, speed)
                        return
                default:
                        haveIncomingSessions := len(IncomingSessions) > 0
                        haveTimerUpper := checkIfTimeToDo(speed)

                        if haveIncomingSessions && haveTimerUpper {
                                ResetMetrics(&IncomingCommands)

                                for _, SessionsAndDatabases := range IncomingSessions {
                                        for session, database := range SessionsAndDatabases {
                                                if session == nil {
                                                        yellow("[doTasks] Sessão nil para %s. Setando status 0.\n", database.InstName)
                                                        AllMetricsFamilyUp.WithLabelValues(database.DbType, database.Name, database.InstName, database.Area).Set(0)
                                                        continue
                                                }

                                                if testDbConnection(session, database) {
                                                        AllMetricsFamilyUp.WithLabelValues(database.DbType, database.Name, database.InstName, database.Area).Set(1)
                                                        checkConditionsToQuery(session, database, IncomingCommands)
                                                } else {
                                                        yellow("[doTasks] Falha na conexão de %s. Setando status 0 e movendo para DisconnectedSessions\n", database.InstName)
                                                        AllMetricsFamilyUp.WithLabelValues(database.DbType, database.Name, database.InstName, database.Area).Set(0)
                                                        DisconnectedSessions[session] = database
                                                        delete(OnlineSessions, session)
                                                }
                                        }
                                }

                                green("\n>>> Todas as queries foram realizadas em speed: %s | dbType: %s\n", speed, dbType)
                                __MapTimers.mapTimersSet("RunningTimers", speed, 0)
                        } else {
                                select {
                                case <-done:
                                        fmt.Printf("[doTasks] Goroutine ended | dbType: %s | speed: %s\n", dbType, speed)
                                        return
                                case <-time.After(100 * time.Millisecond):
                                }
                        }
                }
        }
}
func updateKollectorSettings() {
        // Chama a função para ler o arquivo de configuração do app YAML
        var err error
        KollAppConfigs, KollRunConfigs, err = readSettings(`./KollectorSettings.yml`)
        if err != nil {
                red("[updateKollectorSettings] Erro ao ler configurações: %s\n", err.Error())
                os.Exit(1)
        }
}

func updateKollectorDbs() map[*sql.DB]Database {

        // Chama a função para ler o arquivo YAML
        fmt.Println("[updateKollectorDbs] Fazendo leitura dos arquivos de dbs *.yml")
        var err error
        Databases, err = readYAML()
        if err != nil {
                red("[updateKollectorDbs] Erro durante a importação do arquivo dbs.yml - ", err.Error())
                os.Exit(1)
        }

        RegistredDbs = len(Databases) // contar quantos bancos temos registrados

        // logar em todos os ip's com base no arquivo .yml
        //fmt.Println("[updateKollectorDbs] Fazendo login em todos os bancos disponiveis")
        OnlineSessions = loginAllDbs(Databases) // remover isso depois

        //resetando tempos para coleta imediata
        resetTimeToDo()

        return loginAllDbs(Databases)
}

func updateKollectorQueries(folderState bool) {

        fmt.Println("[updateKollectorQueries] carregando commands dos arquivos *.json")
        commands = jsonLoader() //carregar as querys da pasta 'commands/*.json' na var global 'commands'

        //registrando todas as métricas de fora
        fmt.Println("[updateKollectorQueries] Registrando métricas Gerais...")

        registerMetrics(folderState)

        //resetando tempos para coleta imediata
        resetTimeToDo()
}

// Declaração global da estrutura mapTimers
var __MapTimers = &mapTimers{
        WaitTimers:    make(map[string]int),
        RunningTimers: make(map[string]int),
        HaveTimers:    make(map[string]bool),
}

var readyToStartCollect bool = true // boleano para dar o sinal de partida para a função principal: OrganizeSessions

func main() {

        blue("[Go-Kollector] Iniciando!")

        // Contexto raiz para shutdown gracioso
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()

        // Captura sinais do SO para encerramento gracioso
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

        // >>> NOVO: Carregar configurações do BeyondTrust
        // >>> BeyondTrust (Password Safe) - Auth via PS-Auth header + SignAppin
        BTConfig = BeyondTrustConfig{
                BaseURL: os.Getenv("BT_API_URL"),
                APIKey:  os.Getenv("BT_API_KEY"),
                RunAs:   os.Getenv("BT_RUNAS"),
        }

        if BTConfig.BaseURL == "" || BTConfig.APIKey == "" || BTConfig.RunAs == "" {
                log.Fatal("As variáveis de ambiente do BeyondTrust (BT_API_URL, BT_API_KEY, BT_RUNAS) devem ser configuradas.")
        }
        // atualizar dbs, queries e settings com base nos arquivos
        updateKollectorSettings()

        // inserindo wait timers dinamicamente
        for _, currentRunConfig := range KollRunConfigs {
                __MapTimers.mapTimersSet("WaitTimers", currentRunConfig.Name, *currentRunConfig.WaitSeconds)
        }

        updateKollectorQueries(false)
        updateKollectorDbs()

        // registrar as métricas do próprio coletor e atualizar valores
        fmt.Println("[updateKollectorQueries] Registrando métricas do Self-Monitoring...")
        registerKollectorMetrics()

        fmt.Println("[main] iniciando contador de tempo timerCount() em uma thread separada")
        go timerCount(ctx)

        fmt.Println("[main] iniciando listener para arquivos .yml e .json")
        go func() {
                if err := FileListener(ctx, []string{KollAppConfigs.CommandsDir, KollAppConfigs.DatabasesDir}); err != nil {
                        log.Println("[main] FileListener error:", err)
                }
        }()

        fmt.Println("[main] iniciando endpoint 42000/metrics em uma thread separada como 'async' e registrando metricas")
        srv := &http.Server{Addr: ":42000"}
        go func() {
                http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
                if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
                        log.Println("[main] http server error:", err)
                }
        }()

        green("[main] iniciando endpoints para interface grafica Vue.Js")
        go startKollectorEndpoint(Databases, commands)

        fmt.Println("[main] colocando programa em Loop...")

        loopTicker := time.NewTicker(200 * time.Millisecond)
        defer loopTicker.Stop()

        for {
                select {
                case <-ctx.Done():
                        // parar rotinas internas
                        StopRoutines()
                        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
                        defer shutdownCancel()
                        _ = srv.Shutdown(shutdownCtx)
                        return

                case <-sigCh:
                        fmt.Println("[main] sinal de shutdown recebido, encerrando...")
                        cancel()

                case <-loopTicker.C:
                        if readyToStartCollect {
                                readyToStartCollect = false
                                OrganizeSessions()
                        }
                }
        }
}
