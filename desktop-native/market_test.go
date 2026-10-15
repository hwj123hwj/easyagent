package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func marketFixture() map[string]any {
	return map[string]any{"id": float64(7), "name": "document-design", "display_name": "文档设计", "version": "1.2.0", "description": "生成结构清晰、版式统一的工作文档，支持报告、方案和项目说明。长描述在列表中保持两行，在详情页中完整阅读。", "tags": []any{"文档", "设计", "办公", "排版"}, "featured": true, "install_count": float64(2468), "size_bytes": float64(262144), "section_name": "办公效率", "usage_example": "帮我把这份项目方案整理成结构清晰的文档，保留原有结论。", "rich_description": "根据现有内容组织章节，并检查标题层级、表格和引用。\n\n导出前检查最终版式。", "preview_images": []any{"fixture://preview/1", "fixture://preview/2"}, "example_files": []any{map[string]any{"name": "项目方案.docx", "url": "https://example.com/example.docx", "size": float64(32768)}}}
}
func fixtureMarketPreview(t *testing.T) *preview {
	a := alignmentPreview(t)
	first := marketFixture()
	second := map[string]any{"id": float64(8), "name": "code-review", "display_name": "代码审查", "version": "2.0.1", "description": "检查代码逻辑、边界条件与测试覆盖。", "install_count": float64(829), "size_bytes": float64(16384), "tags": []any{"开发", "审查"}}
	a.Live.Data["market"] = map[string]any{"skills": []any{first, second}, "total": float64(4)}
	a.Live.Data["sections"] = map[string]any{"sections": []any{map[string]any{"id": float64(3), "name": "办公效率"}, map[string]any{"id": float64(4), "name": "开发工具"}}}
	a.Live.Data["installed"] = map[string]any{"installed": []any{map[string]any{"id": float64(8), "name": "code-review", "display_name": "代码审查", "version": "2.0.1"}}}
	m := a.marketState()
	for i, src := range []string{"fixture://preview/1", "fixture://preview/2"} {
		img := image.NewRGBA(image.Rect(0, 0, 640, 400))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{R: 24, G: 38 + uint8(i*14), B: 37, A: 255}}, image.Point{}, draw.Src)
		for row := 0; row < 5; row++ {
			draw.Draw(img, image.Rect(50, 45+row*55, 550-row*36, 65+row*55), &image.Uniform{color.RGBA{R: 75, G: 125, B: 116, A: 255}}, image.Point{}, draw.Src)
		}
		m.Images[src] = ui.NewBitmap(img)
	}
	a.Settings.Open = true
	a.Settings.Tab = "skillMarket"
	return a
}
func TestNativeMarketLayoutsAndDetail(t *testing.T) {
	for _, width := range []int{1240, 900, 640} {
		for _, theme := range []string{"dark", "light"} {
			t.Run(fmt.Sprintf("%d-%s", width, theme), func(t *testing.T) {
				a := fixtureMarketPreview(t)
				a.Settings.Preferences.Theme = theme
				tt := ui.NewTester(a.View, width, 1000)
				first, ok := tt.Find("技能卡片：document-design")
				if !ok || first.X < 240 || first.X+first.W > float32(width) {
					t.Fatalf("card overflows: %+v", first)
				}
				second, _ := tt.Find("技能卡片：code-review")
				if width == 640 && second.Y <= first.Y {
					t.Fatal("compact cards did not stack")
				}
				if width > 640 && (first.Y != second.Y || first.H != second.H) {
					t.Fatalf("grid misalignment: %+v %+v", first, second)
				}
				if !tt.HasText("#文档") || !tt.HasText("256.0 KB") || !tt.HasText("办公效率") {
					t.Fatal("missing card metadata or sections")
				}
				if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
					saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("market-list-%d-%s.png", width, theme)))
				}
				a.openMarketDetail(marketFixture())
				tt.Frame()
				if !tt.HasText("使用示例") || !tt.HasText("产出效果") || !tt.HasText("2468 次安装") {
					t.Fatal("detail lost showcase")
				}
				if dir := os.Getenv("EASYAGENT_PREVIEW_SNAPSHOTS"); dir != "" {
					saveSnapshot(t, tt, filepath.Join(dir, fmt.Sprintf("market-detail-%d-%s.png", width, theme)))
				}
				// Bring previews into view before activating the native lightbox.
				tt.Scroll(float32(width-100), 700, 0, -380)
				tt.Frame()
				click(t, tt, "查看产出预览 1")
				if !a.Live.Market.Lightbox {
					t.Fatal("preview did not open")
				}
				click(t, tt, "下一张")
				if a.Live.Market.PreviewIndex != 1 {
					t.Fatal("lightbox navigation failed")
				}
				tt.Key(0, ui.KeyRight)
				if a.Live.Market.PreviewIndex != 0 {
					t.Fatal("lightbox keyboard navigation failed")
				}
				tt.Key(0, ui.KeyEscape)
				if a.Live.Market.Lightbox {
					t.Fatal("Escape did not dismiss preview")
				}
			})
		}
	}
}
func TestNativeMarketSearchPaginationAndStaleReply(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	requests := make(chan url.Values, 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		requests <- q
		if q.Get("q") == "old" {
			close(entered)
			<-release
		}
		ids := []int{1, 2}
		if q.Get("page") == "2" {
			ids = []int{2, 3}
		}
		items := []any{}
		for _, id := range ids {
			items = append(items, map[string]any{"id": id, "name": fmt.Sprint(id)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": items, "total": 3, "page": q.Get("page"), "query": q.Get("q")})
	}))
	defer srv.Close()
	a := alignmentPreview(t)
	a.Live.LastSave = time.Now().Add(time.Hour)
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	a.Settings.SkillFilter = "热门"
	a.Settings.SkillQuery = "  docs  "
	a.marketState().Section = "3"
	a.searchMarket()
	q := <-requests
	if q.Get("sort") != "installs" || q.Get("q") != "docs" || q.Get("section") != "3" {
		t.Fatalf("wrong filter contract: %v", q)
	}
	waitNative(t, a, func() bool { return !a.Live.Loading["market"] })
	a.searchMarketPage(2)
	<-requests
	waitNative(t, a, func() bool { return !a.Live.Loading["market"] })
	if len(list(object(a.Live.Data["market"])["skills"])) != 3 {
		t.Fatal("pagination duplicates or loses results")
	}
	a.Settings.SkillQuery = "old"
	a.searchMarket()
	<-entered
	a.Settings.SkillQuery = "new"
	a.searchMarket()
	<-requests
	<-requests
	waitNative(t, a, func() bool { return !a.Live.Loading["market"] })
	once.Do(func() { close(release) })
	select {
	case result := <-a.Live.results:
		result.apply()
	case <-time.After(3 * time.Second):
		t.Fatal("old response never returned")
	}
	if str(object(a.Live.Data["market"])["query"]) != "new" {
		t.Fatal("stale search overwrote current results")
	}

}
func TestNativeMarketCardActionsAndPerSkillProgress(t *testing.T) {
	var starts, deletes atomic.Int32
	var canceled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/skills/market/install" {
			starts.Add(1)
			fmt.Fprint(w, `{"job_id":"job-7"}`)
			return
		}
		if r.URL.Path == "/skills/market/jobs/job-7/cancel" {
			canceled.Store(true)
			fmt.Fprint(w, `{}`)
			return
		}
		if r.URL.Path == "/skills/market/jobs/job-7" {
			if canceled.Load() {
				fmt.Fprint(w, `{"id":"job-7","skill_id":7,"state":"failed","error":"已取消"}`)
			} else {
				fmt.Fprint(w, `{"id":"job-7","skill_id":7,"state":"running","phase":"downloading","bytes_done":5,"bytes_total":10}`)
			}
			return
		}
		if r.Method == "DELETE" && r.URL.Path == "/skills/market/installed/code-review" {
			deletes.Add(1)
			fmt.Fprint(w, `{}`)
			return
		}
		if r.URL.Path == "/skills/market/installed" {
			if deletes.Load() > 0 {
				fmt.Fprint(w, `{"installed":[]}`)
			} else {
				fmt.Fprint(w, `{"installed":[{"id":8,"name":"code-review","display_name":"代码审查","version":"2.0.1"}]}`)
			}
			return
		}

		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	a := fixtureMarketPreview(t)
	a.Live.Client, _ = newBackendClient(srv.URL, "")
	tt := ui.NewTester(a.View, 1240, 1000)
	click(t, tt, "安装")
	if a.Settings.SkillDetail != "" {
		t.Fatal("install click bubbled into detail")
	}
	a.installSkill(marketFixture())
	waitNative(t, a, func() bool { return str(a.marketState().Jobs["7"]["phase"]) == "downloading" })
	if starts.Load() != 1 {
		t.Fatalf("duplicate starts: %d", starts.Load())
	}
	if a.marketState().Jobs["8"] != nil {
		t.Fatal("install state leaked into another card")
	}
	tt.Frame()
	if !tt.HasText("下载中 50%") {
		t.Fatal("per-card progress missing")
	}
	click(t, tt, "技能卡片：document-design")
	if a.Settings.SkillDetail != "7" {
		t.Fatal("card did not open detail")
	}
	a.cancelMarketInstall("7", "job-7")
	waitNative(t, a, func() bool {
		return str(a.marketState().Jobs["7"]["state"]) == "failed" && !a.Live.Loading["installed"]
	})
	a.Settings.SkillDetail = ""
	a.Settings.SkillFilter = "已安装"
	tt.Frame()
	click(t, tt, "卸载")
	if deletes.Load() != 0 || a.Live.Market.ConfirmName != "code-review" {
		t.Fatal("uninstall skipped confirmation")
	}
	click(t, tt, "取消")
	if deletes.Load() != 0 || a.Live.Market.ConfirmName != "" {
		t.Fatal("uninstall cancel failed")
	}
	click(t, tt, "卸载")
	click(t, tt, "确认卸载")
	waitNative(t, a, func() bool {
		return deletes.Load() == 1 && !a.Live.Loading["installed"] && !a.Live.Loading["uninstall/code-review"] && len(list(object(a.Live.Data["installed"])["installed"])) == 0
	})
	tt.Frame()
	if !tt.HasText("还没有已安装的技能") {
		t.Fatal("uninstall did not refresh installed view")
	}

}
func TestNativeMarketPublicImageTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("backend token leaked to public image")
		}
		_ = png.Encode(w, image.NewRGBA(image.Rect(0, 0, 16, 12)))
	}))
	defer srv.Close()
	img, err := fetchMarketImage(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	w, h := img.Size()
	if w != 16 || h != 12 {
		t.Fatal("image decode failed")
	}
	if _, err := fetchMarketImage(context.Background(), "file:///etc/hosts"); err == nil {
		t.Fatal("unexpected local asset read")
	}
}
