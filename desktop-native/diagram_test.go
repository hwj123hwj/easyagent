package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNativeDiagramRendering(t *testing.T) {
	for index, src := range []string{"graph TD\n A[开始] --> B[结束]", "sequenceDiagram\n Alice->>Bob: Hello\n Bob-->>Alice: Done", "pie title Usage\n \"Go\" : 80\n \"Other\" : 20"} {
		for _, dark := range []bool{false, true} {
			img, err := renderDiagram(src, dark)
			if err != nil {
				t.Fatal(src, err)
			}
			w, h := img.Size()
			if w <= 0 || h <= 0 {
				t.Fatal("empty diagram")
			}
			tt := ui.NewTester(func(c *ui.Context) { ui.Image(c, img).Size(float32(w), float32(h)) }, w, h)
			if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
				saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("diagram-%d-%v.png", index, dark)))
			}
			if tt.Image() == nil {
				t.Fatal("diagram not drawable")
			}
		}
	}
	if _, err := renderDiagram("unsupportedDiagram\n junk", false); err == nil {
		t.Fatal("unsupported diagram silently accepted")
	}
	paths, err := diagramTextPaths([]byte(`<svg viewBox="0 0 200 100"><text x="100" y="20" text-anchor="middle">开始</text></svg>`))
	if err != nil || bytes.Contains(paths, []byte("<text")) || !bytes.Contains(paths, []byte(" d=\"M")) {
		t.Fatal("labels not converted to paths", err, string(paths))
	}
}
