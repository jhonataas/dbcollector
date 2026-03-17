package main

import (
        "context"
        "log"
        "path/filepath"
        "time"

        "github.com/fsnotify/fsnotify"
)

// FileListener observa alterações nas pastas informadas e dispara reload das configs (debounced).
func FileListener(ctx context.Context, folders []string) error {
        watcher, err := fsnotify.NewWatcher()
        if err != nil {
                return err
        }
        defer watcher.Close()

        for _, folder := range folders {
                if folder == "" {
                        continue
                }
                if err := watcher.Add(folder); err != nil {
                        return err
                }
        }

        var debounce *time.Timer
        debounceDur := 600 * time.Millisecond

        reload := func(path string) {
                // Só reage a arquivos relevantes
                ext := filepath.Ext(path)
                if ext != ".yml" && ext != ".yaml" && ext != ".json" {
                        return
                }

                // Recarrega configs e força re-organização
                log.Printf("[FileListener] alteração detectada em %s - recarregando configs\n", path)

                // Se mexeu em commands -> recarrega métricas/queries
                updateKollectorQueries(true)

                // Se mexeu em databases -> recarrega dbs e sessões
                updateKollectorDbs()

                // Força reinício do agendamento
                readyToStartCollect = true
        }

        for {
                select {
                case <-ctx.Done():
                        return nil

                case ev, ok := <-watcher.Events:
                        if !ok {
                                return nil
                        }
                        // Ignore chmod
                        if ev.Op&fsnotify.Chmod == fsnotify.Chmod {
                                continue
                        }

                        // Debounce
                        if debounce != nil {
                                debounce.Stop()
                        }
                        path := ev.Name
                        debounce = time.AfterFunc(debounceDur, func() { reload(path) })

                case err, ok := <-watcher.Errors:
                        if !ok {
                                return nil
                        }
                        log.Println("[FileListener] watcher error:", err)
                }
        }
}
