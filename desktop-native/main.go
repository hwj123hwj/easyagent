package main

import (
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/plugins/updater"
	nativeupdater "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"log"
	"os"
	"time"
)

func main() {
	mygo.Use(nativeupdater.Plugin)
	app := mygo.App
	app.SetLanguages("zh-Hans", "en")
	app.SetName("EasyAgent")
	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}))
		dir, err := app.Path(mygo.PathUserData)
		if override := os.Getenv("EASYAGENT_NATIVE_DATA"); override != "" {
			dir = override
			err = nil
		}
		if err != nil {
			log.Fatal("无法定位原生应用数据目录")
		}
		state := newConnectedPreview(dir)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "EasyAgent", Content: ui.View(state.View), Width: 1240, Height: 820,
			MinWidth: 900, MinHeight: 640, TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 48,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 17}, BackgroundColor: "#131718",
		})
		state.Window = win
		state.UpdaterReady = true
		offUpdates := updater.OnChange(win.Invalidate)
		offQuit := app.OnBeforeQuit(func(e *mygo.QuitEvent) {
			if reason := state.quitBlockReason(); reason != "" {
				e.PreventDefault()
				state.Live.Error = reason
				win.Show()
				win.Invalidate()
			}
		})
		state.syncNativeLocale()
		done := make(chan struct{})
		win.OnClosed(func() { close(done); offUpdates(); offQuit(); state.closeLive() })
		go func() {
			ticker := time.NewTicker(60 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					// Wait for the UI thread: at most one tick can be queued even
					// while a document is expensive to lay out. Idle ticks do not
					// invalidate the text/layout tree.
					mygo.RunOnMain(func() {
						if !win.IsDestroyed() && state.tick() {
							win.Invalidate()
						}
					})
				}
			}
		}()
		win.Show()
		state.connectLive()
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
