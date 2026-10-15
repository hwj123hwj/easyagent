package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

func (a *preview) imageThumbnail(c *ui.Context, key, name string, item messageImage, sid string) {
	l := a.Live
	if image, known := l.Images[key]; known && image != nil {
		ui.ButtonBase(c).Label("预览 " + name).MinWidth(0).OnClick(func() { l.ImagePreview = image; l.ImageTitle = name; l.ImageZoom = 1 }).Children(func() {
			w, h := nativeImageSize(image)
			scale := min(float32(1), min(360/float32(w), 240/float32(h)))
			ui.Image(c, image).Size(float32(w)*scale, float32(h)*scale).Fit(ui.Contain)
		})
		return
	}
	operation := "image/" + key
	if err := l.ResourceErrors[operation]; err != "" {
		ui.Row(c).Gap(8).Children(func() {
			a.note(c, "图片无法读取")
			a.action(c, "重试图片").OnClick(func() { delete(l.ResourceErrors, operation); delete(l.Images, key) })
		})
		return
	}
	if !l.Loading[operation] {
		type loaded struct {
			Bitmap ui.ImageSource
			Error  error
		}
		a.run(operation, func(ctx context.Context, b *backendClient) (any, error) {
			var data []byte
			var err error
			if item.DataURL != "" {
				_, encoded, ok := stringsCutDataURL(item.DataURL)
				if !ok || len(encoded) > 8<<20 {
					return loaded{Error: errors.New("图片数据无效")}, nil
				}
				data, err = base64.StdEncoding.DecodeString(encoded)
			} else if item.AttachmentID != "" && b != nil {
				target := item.SessionID
				if target == "" {
					target = sid
				}
				data, err = b.download(ctx, sessionPath(target)+"/attachments/"+pathSegment(item.AttachmentID)+"/raw")
			} else {
				err = errors.New("图片没有可用的数据")
			}
			var image ui.ImageSource
			if err == nil {
				image, err = decodeNativeImage(data, name)
			}
			return loaded{image, err}, nil
		}, func(v any) {
			result := v.(loaded)
			if result.Error != nil {
				l.ResourceErrors[operation] = result.Error.Error()
				return
			}
			l.Images[key] = result.Bitmap
		})
	}
	a.note(c, "加载图片…")
}
func stringsCutDataURL(raw string) (string, string, bool) {
	for i, r := range raw {
		if r == ',' {
			return raw[:i], raw[i+1:], true
		}
	}
	return "", "", false
}
func (a *preview) attachmentChips(c *ui.Context, s *session) {
	for index, att := range s.Attachments {
		ui.Column(c.Key(fmt.Sprint(index))).Gap(4).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				icon(c, "file")
				ui.Text(c, str(att["name"])).FontSize(11).Grow(1).MinWidth(0).SingleLine()
				a.note(c, marketBytes(att["size"]))
				iconButton(c, "x", "移除附件："+str(att["name"])).Disabled(s.Sending || a.Live.Loading["queue-add/"+s.RemoteID]).Size(22, 22).OnClick(func() { s.Attachments = append(s.Attachments[:index], s.Attachments[index+1:]...) })
			})
			if mime := str(att["mime_type"]); len(mime) >= 6 && mime[:6] == "image/" {
				id := str(att["id"])
				key := a.Live.Config.ActiveHost + "/" + s.RemoteID + "/" + id
				a.imageThumbnail(c, key, str(att["name"]), messageImage{AttachmentID: id, SessionID: s.RemoteID}, s.RemoteID)
			}
		})
	}
}
func (a *preview) imageLightbox(c *ui.Context) {
	l := a.Live
	open := l.ImagePreview != nil
	ui.Modal(c, &open, func() {
		if l.ImagePreview == nil {
			return
		}
		image := l.ImagePreview
		width, height := c.Size()
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, l.ImageTitle).FontSize(14).Grow(1).MinWidth(0).SingleLine()
			flatButton(c, "", "−").Label("缩小图片").OnClick(func() { l.ImageZoom = max(.25, l.ImageZoom-.25) })
			flatButton(c, "", fmt.Sprintf("%.0f%%", l.ImageZoom*100)).OnClick(func() { l.ImageZoom = 1 })
			flatButton(c, "", "+").Label("放大图片").OnClick(func() { l.ImageZoom = min(4, l.ImageZoom+.25) })
			iconButton(c, "x", "关闭图片预览").OnClick(func() { l.ImagePreview = nil })
		})
		ui.Scroll(c).Width(max(200, width-120)).Height(max(180, height-190)).Children(func() {
			ui.ScrollHorizontal(c).Children(func() {
				w, h := nativeImageSize(image)
				scale := min((width-160)/float32(w), (height-230)/float32(h)) * l.ImageZoom
				ui.Image(c, image).Width(float32(w) * scale).Height(float32(h) * scale)
			})
		})
	})
	if !open {
		l.ImagePreview = nil
	}
}
func (a *preview) pasteAttachments(services ui.Services, s *session) bool {
	data := services.ReadClipboardData(transfer.FileList, transfer.URIList, transfer.PNG)
	if paths, err := data.Files(); err == nil && len(paths) > 0 {
		a.attachNativeFiles(s, paths)
		return true
	}
	if image, err := data.Read(transfer.PNG); err == nil && len(image) > 0 {
		a.attachClipboardImage(s, image)
		return true
	}
	return false
}
func (a *preview) attachClipboardImage(s *session, data []byte) {
	if len(data) > 4<<20 {
		a.Live.Error = "单个附件必须为 1 字节至 4 MiB"
		return
	}
	if !a.Live.Connected || s.Archived || s.Sending || a.Live.Loading["queue-add/"+s.RemoteID] {
		a.Live.Error = "当前无法添加附件"
		return
	}
	if len(s.Attachments) >= 8 {
		a.Live.Error = "最多添加 8 个附件"
		return
	}
	if s.RemoteID == "" {
		workspace := s.Workspace
		if workspace == "" {
			workspace = a.Live.workspace()
		}
		a.createLiveSessionIn("", workspace, s.Application, func(created *session) { a.attachClipboardImage(created, data) })
		return
	}
	a.run("attachments/"+s.RemoteID, func(ctx context.Context, b *backendClient) (any, error) {
		var result map[string]any
		err := b.request(ctx, "POST", sessionPath(s.RemoteID)+"/attachments", map[string]any{"name": "clipboard.png", "data": base64.StdEncoding.EncodeToString(data)}, &result)
		return result, err
	}, func(v any) { s.Attachments = append(s.Attachments, v.(map[string]any)) })
}

func nativeImageSize(src ui.ImageSource) (float32, float32) {
	switch im := src.(type) {
	case *ui.Bitmap:
		w, h := im.Size()
		return float32(w), float32(h)
	case *ui.SVG:
		return im.Size()
	}
	return 1, 1
}
