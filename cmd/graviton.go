package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/telemetrytv/graviton-cli/cmd/commands"
)

func main() {
	if !(runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		fmt.Println("Graviton only supports amd64 architecture")
		return
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		fmt.Println("Graviton only supports linux and macOS operating systems")
		return
	}

	if err := commands.Execute(); err != nil {
		fmt.Printf("Graviton has errored unexpectedly %s", err)
		os.Exit(1)
	}
}
