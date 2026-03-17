package main

import "fmt"

// startKollectorEndpoint existed in the original project for a Vue.js UI.
// For the Docker/production collector image, the UI is optional.
// This stub keeps the build green; implement the real UI server in a separate build/tag if needed.
func startKollectorEndpoint(_ []Database, _ Commands) {
        fmt.Println("[startKollectorEndpoint] UI endpoint disabled (stub).")
}
