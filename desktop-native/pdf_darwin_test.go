//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func pdfFixture() []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	stream := "1 0 0 rg 0 50 100 50 re f\n0 0 1 rg 0 0 100 50 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R >>",
	}
	offsets := []int{0}
	for i, v := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, v)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes()
}
func TestNativePDFPagesAndOrientation(t *testing.T) {
	data := pdfFixture()
	page, err := renderNativePDF(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.Image == nil {
		t.Fatal("missing PDF pages", page.Count)
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Image(c, page.Image).Size(200, 200) }, 200, 200)
	img := tt.Image()
	r, _, b, _ := img.At(100, 25).RGBA()
	if r < b {
		t.Fatal("PDF top half is upside down")
	}
	r, _, b, _ = img.At(100, 175).RGBA()
	if b < r {
		t.Fatal("PDF bottom half is upside down")
	}
	next, err := renderNativePDF(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	w, h := next.Image.Size()
	if w != 2*h {
		t.Fatal("PDF aspect ratio changed", w, h)
	}
	if _, err = renderNativePDF(data, 2); err == nil {
		t.Fatal("out of bounds page accepted")
	}
	if _, err = renderNativePDF([]byte("invalid"), 0); err == nil {
		t.Fatal("invalid PDF accepted")
	}
}
