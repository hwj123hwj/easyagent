//go:build !darwin

package main

import (
	"errors"
	"github.com/egoist/mygo/ui"
)

type nativePDFPage struct {
	Image *ui.Bitmap
	Count int
}

func renderNativePDF([]byte, int) (nativePDFPage, error) {
	return nativePDFPage{}, errors.New("当前系统未提供原生 PDF 引擎，可导出后查看")
}
