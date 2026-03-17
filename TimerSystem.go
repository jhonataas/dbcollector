package main

import (
        "context"
        "fmt"
        "sort"
        "strings"
        "sync"
        "time"
)

type mapTimers struct {
        sync.Mutex
        WaitTimers    map[string]int
        RunningTimers map[string]int
        HaveTimers    map[string]bool
}

func (__map *mapTimers) mapTimersGet(timerType string, key string) interface{} {

        __map.Lock()
        defer __map.Unlock()

        switch timerType {
        case "WaitTimers":
                if value, has := __map.WaitTimers[key]; has {
                        return value
                }
        case "RunningTimers":
                if value, has := __map.RunningTimers[key]; has {
                        return value
                }
        case "HaveTimers":
                if value, has := __map.HaveTimers[key]; has {
                        return value
                }
        default:
                return nil
        }
        return nil
}

func (__map *mapTimers) mapTimersSet(timerType string, key string, value interface{}) {

        __map.Lock()
        defer __map.Unlock()

        switch timerType {
        case "WaitTimers":
                __map.WaitTimers[key] = value.(int)
        case "RunningTimers":
                __map.RunningTimers[key] = value.(int)
        case "HaveTimers":
                __map.HaveTimers[key] = value.(bool)
        default:
        }
}

func (__map *mapTimers) mapTimersIncrease(key string, value int) {

        __map.Lock()
        defer __map.Unlock()

        __map.RunningTimers[key] += value
}

func (__map *mapTimers) mapTimersList(timerType string) interface{} {

        __map.Lock()
        defer __map.Unlock()

        switch timerType {
        case "WaitTimers":
                return __map.WaitTimers
        case "RunningTimers":
                return __map.RunningTimers
        case "HaveTimers":
                return __map.HaveTimers
        default:
                return nil
        }
}

func printTimers() {

        var keys []string
        for key := range __MapTimers.mapTimersList("HaveTimers").(map[string]bool) {
                keys = append(keys, key)
        }
        sort.Strings(keys)

        var sb strings.Builder
        sb.WriteString("[timerCount] Timers: ")

        for _, key := range keys {
                sb.WriteString(fmt.Sprintf("%s[%d] ", key, __MapTimers.mapTimersGet("RunningTimers", key).(int)))

        }

        sb.WriteString(fmt.Sprintf("kollector_metrics[%d] disconnected_sessions[%d] || Sessions: Online[%d] Disconnected[%d]\n",
                timer_kollector_metrics, timer_disconnected_sessions, len(OnlineSessions), len(DisconnectedSessions)))

        blue(sb.String())
}

// função do contador de tempo que sempre adiciona 1 em 1 segundo a cada timer
func timerCount(ctx context.Context) {

        //resetTimeToDo()

        // temporizadores que vão ficar resetando nos loops

        for {
                select {
                case <-ctx.Done():
                        return
                default:
                }
                // só vai aumentar o valor dos contadores encontrados e se houver sessões online
                // caso não tenha nenhuma sessão online, vai resetar todos os timers.

                if len(OnlineSessions) == 0 {
                        for _, currentRunConfig := range KollRunConfigs {
                                __MapTimers.mapTimersSet("RunningTimers", currentRunConfig.Name, 0)
                        }
                }

                for currentHaveTimerName, currentHaveTimerBool := range __MapTimers.mapTimersList("HaveTimers").(map[string]bool) {
                        //fmt.Printf("currentHaveTimerName: %s || RunningTimers[currentHaveTimerName]: %d\n", currentHaveTimerName, RunningTimers[currentHaveTimerName])
                        if currentHaveTimerBool {
                                __MapTimers.mapTimersIncrease(currentHaveTimerName, 1)
                        } else {
                                __MapTimers.mapTimersSet("RunningTimers", currentHaveTimerName, 0)
                        }
                }

                if len(KollectorMetrics) > 0 {
                        timer_kollector_metrics += 1
                }
                if len(DisconnectedSessions) > 0 {
                        timer_disconnected_sessions += 1
                }

                printTimers()

                time.Sleep(1 * time.Second)
        }
}
