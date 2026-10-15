package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/audio"
	_ "github.com/egoist/mygo/audio/mp3"
	_ "github.com/egoist/mygo/audio/vorbis"
	"github.com/egoist/mygo/ui"
)

type messageImage struct {
	DataURL      string `json:"data_url"`
	AttachmentID string `json:"attachmentId"`
	SessionID    string `json:"sessionId"`
}
type nativeMusic struct {
	Player                *audio.Player
	Title, Artist, SongID string
	Source                string
	Volume                float64
	Lyrics                string
	LyricsOpen            bool
}

func (b *backendClient) download(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.URL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	res, err := b.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("媒体请求失败（HTTP %d）", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
	if len(data) > 32<<20 {
		return nil, errors.New("媒体超过 32 MiB 限制")
	}
	return data, err
}
func (a *preview) messageImages(c *ui.Context, m *message) {
	for i, item := range m.Images {
		key := item.DataURL
		if key == "" {
			key = a.Live.Config.ActiveHost + "/" + a.current().RemoteID + "/" + item.AttachmentID
		}
		a.imageThumbnail(c.Key(i), key, "附件图片", item, a.current().RemoteID)
	}
}
func (a *preview) musicCard(c *ui.Context, m *message) {
	if m.Tool != "music_play" && str(m.Details["proxy_url"]) == "" && str(m.Details["direct_url"]) == "" {
		return
	}
	details := nativeMusicDetails(m)
	if str(details["proxy_url"]) == "" && str(details["direct_url"]) == "" {
		return
	}
	title, artist := str(details["song_name"]), str(details["artist"])

	ui.Row(c).Gap(8).Padding(10).Background(c.Theme().Surface).Radius(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() { ui.Text(c, title).Bold(); a.note(c, artist) })
		a.action(c, "播放").Disabled(a.Live.Loading["music"]).OnClick(func() { a.playNativeMusic(details) })
	})
}
func (a *preview) playNativeMusic(details map[string]any) {
	l := a.Live
	if l.Loading["music"] {
		return
	}
	source := str(details["proxy_url"]) + str(details["direct_url"])
	if l.Music.Player != nil && l.Music.Source == source {
		if l.Music.Player.Playing() {
			l.Music.Player.Pause()
		} else {
			l.Music.Player.Play()
		}
		return
	}
	title, artist, song := str(details["song_name"]), str(details["artist"]), str(details["song_id"])
	proxy, direct := str(details["proxy_url"]), str(details["direct_url"])
	a.run("music", func(ctx context.Context, b *backendClient) (any, error) {
		var data []byte
		var err error
		if proxy != "" {
			u, e := url.Parse(proxy)
			if e != nil || !strings.HasPrefix(u.Path, "/music/audio/") {
				return nil, errors.New("音频代理地址无效")
			}
			data, err = b.download(ctx, u.RequestURI())
		} else {
			u, e := url.Parse(direct)
			if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
				return nil, errors.New("音频地址无效")
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", direct, nil)
			res, e := (&http.Client{Timeout: 30 * time.Second}).Do(req)
			if e != nil {
				return nil, e
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("音频请求失败（HTTP %d）", res.StatusCode)
			}
			data, err = io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
			if len(data) > 32<<20 {
				return nil, errors.New("音频超过大小限制")
			}
		}
		if err != nil {
			return nil, err
		}
		return audio.OpenStream(bytes.NewReader(data))
	}, func(v any) {
		p, _ := v.(*audio.Player)
		if p == nil {
			return
		}
		if l.Music.Player != nil {
			l.Music.Player.Close()
		}
		l.Music = nativeMusic{Player: p, Title: title, Artist: artist, SongID: song, Source: source, Volume: 1}
		p.Play()
		if song != "" {
			a.call("GET", "/music/lyrics/"+pathSegment(song), nil, func(v any) {
				if l.Music.Player == p && l.Music.SongID == song {
					l.Music.Lyrics = lyricsText(v)
				}
			})
		}
	})
}
func (a *preview) musicBar(c *ui.Context) {
	m := &a.Live.Music
	if m.Player == nil {
		return
	}
	ui.Column(c).Background(c.Theme().Surface).Children(func() {
		if m.LyricsOpen {
			ui.Scroll(c).MaxHeight(220).Padding(12, 20).Children(func() {
				if m.Lyrics == "" {
					a.note(c, "暂无歌词")
				} else {
					nativePre(c, m.Lyrics)
				}
			})
		}
		ui.Row(c).Gap(8).Padding(6, 16).Background(c.Theme().Surface).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
			ui.Text(c, m.Title+" · "+m.Artist).FontSize(12).MaxLines(1).MinWidth(0)
			label := "播放"
			if m.Player.Playing() {
				label = "暂停"
			}
			a.action(c, label).OnClick(func() {
				if m.Player.Playing() {
					m.Player.Pause()
				} else {
					m.Player.Play()
				}
			})
			position := m.Player.Position().Seconds()
			duration := m.Player.Duration().Seconds()
			ui.Text(c, fmt.Sprintf("%s / %s", musicTime(position), musicTime(duration))).FontSize(10).TextColor(c.Theme().TextMuted)
			if duration > 0 {
				if ui.Slider(c, &position, 0, duration).Grow(1).MinWidth(30).Label("播放进度").Changed() {
					m.Player.Seek(time.Duration(position * float64(time.Second)))
				}
			}
			if ui.Slider(c, &m.Volume, 0, 1).Width(65).Label("音量").Changed() {
				m.Player.SetVolume(m.Volume)
			}
			a.action(c, "歌词").OnClick(func() { m.LyricsOpen = !m.LyricsOpen })
			iconButton(c, "x", "关闭播放器").OnClick(func() { m.Player.Close(); m.Player = nil })
		})
	})
}
func lyricsText(v any) string {
	if text, ok := v.(string); ok {
		return text
	}
	m := object(v)
	for _, key := range []string{"lyrics", "lyric", "lrc", "text"} {
		if text := str(m[key]); text != "" {
			return text
		}
		if nested := object(m[key]); nested != nil {
			if text := str(nested["lyric"]); text != "" {
				return text
			}
		}
	}
	return ""
}

func nativeMusicDetails(m *message) map[string]any {
	details := map[string]any{}
	for k, v := range m.Details {
		details[k] = v
	}
	for key, pattern := range nativeMusicPatterns {
		if str(details[key]) == "" {
			if match := pattern.FindStringSubmatch(m.Result); len(match) > 1 {
				details[key] = strings.TrimSpace(match[1])
			}
		}
	}
	if match := nativePlayingPattern.FindStringSubmatch(m.Result); len(match) > 2 {
		if str(details["song_name"]) == "" {
			details["song_name"] = match[1]
		}
		if str(details["artist"]) == "" {
			details["artist"] = match[2]
		}
	}
	return details
}
func musicTime(seconds float64) string {
	return fmt.Sprintf("%d:%02d", int(seconds)/60, int(seconds)%60)
}

var nativeMusicPatterns = map[string]*regexp.Regexp{
	"song_name":  regexp.MustCompile(`(?m)曲名[：:]\s*(.+)$`),
	"artist":     regexp.MustCompile(`(?m)歌手[：:]\s*(.+)$`),
	"proxy_url":  regexp.MustCompile(`(?m)(?:Proxy URL:|播放链接[：:])\s*(\S+)`),
	"direct_url": regexp.MustCompile(`(?m)Direct URL:\s*(\S+)`),
}
var nativePlayingPattern = regexp.MustCompile(`(?m)Now playing:\s*(.+?)\s*[—–-]\s*(.+)$`)
