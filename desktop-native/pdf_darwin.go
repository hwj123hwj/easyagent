//go:build darwin

package main

import (
	"errors"
	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/ui"
	"image"
	"math"
	"runtime"
	"sync"
)

type pdfRect struct{ X, Y, W, H float64 }
type pdfTransform struct{ A, B, C, D, TX, TY float64 }
type nativePDFPage struct {
	Image *ui.Bitmap
	Count int
}

var pdfAPI struct {
	once            sync.Once
	err             error
	data            func(uintptr, *byte, int64) uintptr
	release         func(uintptr)
	provider        func(uintptr) uintptr
	providerRelease func(uintptr)
	document        func(uintptr) uintptr
	documentRelease func(uintptr)
	pages           func(uintptr) uint64
	page            func(uintptr, uint64) uintptr
	box             func(uintptr, int32) pdfRect
	transform       func(uintptr, int32, pdfRect, int32, bool) pdfTransform
	colorSpace      func() uintptr
	colorRelease    func(uintptr)
	bitmap          func(*byte, uint64, uint64, uint64, uint64, uintptr, uint32) uintptr
	contextRelease  func(uintptr)
	fillColor       func(uintptr, float64, float64, float64, float64)
	fill            func(uintptr, pdfRect)
	translate       func(uintptr, float64, float64)
	scale           func(uintptr, float64, float64)
	concat          func(uintptr, pdfTransform)
	draw            func(uintptr, uintptr)
}

func loadPDFAPI() error {
	pdfAPI.once.Do(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			pdfAPI.err = err
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			pdfAPI.err = err
			return
		}
		p := &pdfAPI
		purego.RegisterLibFunc(&p.data, cf, "CFDataCreate")
		purego.RegisterLibFunc(&p.release, cf, "CFRelease")
		for _, entry := range []struct {
			fn   any
			name string
		}{{&p.provider, "CGDataProviderCreateWithCFData"}, {&p.providerRelease, "CGDataProviderRelease"}, {&p.document, "CGPDFDocumentCreateWithProvider"}, {&p.documentRelease, "CGPDFDocumentRelease"}, {&p.pages, "CGPDFDocumentGetNumberOfPages"}, {&p.page, "CGPDFDocumentGetPage"}, {&p.box, "CGPDFPageGetBoxRect"}, {&p.transform, "CGPDFPageGetDrawingTransform"}, {&p.colorSpace, "CGColorSpaceCreateDeviceRGB"}, {&p.colorRelease, "CGColorSpaceRelease"}, {&p.bitmap, "CGBitmapContextCreate"}, {&p.contextRelease, "CGContextRelease"}, {&p.fillColor, "CGContextSetRGBFillColor"}, {&p.fill, "CGContextFillRect"}, {&p.translate, "CGContextTranslateCTM"}, {&p.scale, "CGContextScaleCTM"}, {&p.concat, "CGContextConcatCTM"}, {&p.draw, "CGContextDrawPDFPage"}} {
			purego.RegisterLibFunc(entry.fn, cg, entry.name)
		}
	})
	return pdfAPI.err
}
func renderNativePDF(data []byte, index int) (nativePDFPage, error) {
	var result nativePDFPage
	if len(data) == 0 {
		return result, errors.New("PDF 内容为空")
	}
	if err := loadPDFAPI(); err != nil {
		return result, err
	}
	p := &pdfAPI
	cf := p.data(0, &data[0], int64(len(data)))
	if cf == 0 {
		return result, errors.New("无法读取 PDF")
	}
	defer p.release(cf)
	provider := p.provider(cf)
	if provider == 0 {
		return result, errors.New("无法读取 PDF 数据")
	}
	defer p.providerRelease(provider)
	doc := p.document(provider)
	if doc == 0 {
		return result, errors.New("PDF 格式无效或需要密码")
	}
	defer p.documentRelease(doc)
	result.Count = int(p.pages(doc))
	if index < 0 || index >= result.Count {
		return result, errors.New("PDF 页码超出范围")
	}
	page := p.page(doc, uint64(index+1))
	box := p.box(page, 1)
	if box.W <= 0 || box.H <= 0 || math.IsNaN(box.W) || math.IsNaN(box.H) || math.IsInf(box.W, 0) || math.IsInf(box.H, 0) {
		return result, errors.New("PDF 页面尺寸无效")
	}
	scale := math.Min(1400/box.W, 2200/box.H)
	width, height := max(1, int(box.W*scale)), max(1, int(box.H*scale))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	space := p.colorSpace()
	defer p.colorRelease(space)
	ctx := p.bitmap(&img.Pix[0], uint64(width), uint64(height), 8, uint64(img.Stride), space, 0x4001)
	if ctx == 0 {
		return result, errors.New("无法创建 PDF 预览")
	}
	defer p.contextRelease(ctx)
	area := pdfRect{W: float64(width), H: float64(height)}
	p.fillColor(ctx, 1, 1, 1, 1)
	p.fill(ctx, area)
	p.translate(ctx, 0, float64(height))
	p.scale(ctx, 1, -1)
	p.concat(ctx, p.transform(page, 1, area, 0, true))
	p.draw(ctx, page)
	runtime.KeepAlive(data)
	runtime.KeepAlive(img)
	result.Image = ui.NewBitmap(img)
	return result, nil
}
