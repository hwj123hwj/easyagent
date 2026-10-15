// Build the Go core/resources before MyGo packages the native UI. Keeping this
// outside mygo.json's frontend buildCommand avoids enabling frontend bindings.
package main

import (
	"os"
	"os/exec"
)

func main() {
	buildArgs := append([]string{"run", "github.com/egoist/mygo/cmd/mygo", "build"}, os.Args[1:]...)
	for _, args := range [][]string{{"run", "./tools/prepare"}, buildArgs} {
		cmd := exec.Command("go", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	}
}
