package main

import (
	"io"
	"os"
	"testing"
)

func TestVersionWithoutConfiguration(t *testing.T) {
	oldArgs, oldStdout, oldVersion := os.Args, os.Stdout, version
	defer func() { os.Args, os.Stdout, version = oldArgs, oldStdout, oldVersion }()
	for _, flag := range []string{"--version", "-version"} {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = []string{"easyagent-bridge", flag}
		os.Stdout, version = writer, "v1.2.3-rc.1"
		main() // Must return before credentials, network or service initialization.
		writer.Close()
		output, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(output) != "easyagent-bridge v1.2.3-rc.1\n" {
			t.Fatalf("unexpected version: %q", output)
		}
	}
}
