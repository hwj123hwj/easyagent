package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

type voiceRecorder interface {
	Stop() ([]byte, error)
	Levels() [6]float32
}

func pcmWAV(pcm []byte) []byte {
	data := make([]byte, 44+len(pcm))
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+len(pcm)))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 16000)
	binary.LittleEndian.PutUint32(data[28:], 32000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(pcm)))
	copy(data[44:], pcm)
	return data
}
func (a *preview) pickAttachments() {
	origin := a.current()
	a.background(func(context.Context, *backendClient) (any, error) {
		return mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择附件", Multiple: true, Parent: a.Window})
	}, func(v any) {
		paths, _ := v.([]string)
		if len(paths) == 0 {
			return
		}
		if a.current() != origin {
			a.Live.Error = "会话已切换，请在目标会话重新添加附件"
			return
		}
		a.attachNativeFiles(origin, paths)
	})
}
func (a *preview) attachNativeFiles(s *session, paths []string) {
	if len(paths) == 0 || !a.Live.Connected || s.Archived || s.Sending || a.Live.Loading["queue-add/"+s.RemoteID] {
		return
	}
	if s.RemoteID == "" {
		workspace := s.Workspace
		if workspace == "" {
			workspace = a.Live.workspace()
		}
		a.createLiveSessionIn("", workspace, s.Application, func(created *session) { a.attachNativeFiles(created, paths) })
		return
	}
	limit := 8 - len(s.Attachments)
	if limit <= 0 {
		a.Live.Error = "最多添加 8 个附件"
		return
	}
	type uploadResult struct {
		items  []map[string]any
		errors []string
	}
	a.run("attachments/"+s.RemoteID, func(ctx context.Context, b *backendClient) (any, error) {
		result := uploadResult{}
		if len(paths) > limit {
			result.errors = append(result.errors, "最多添加 8 个附件")
		}
		for _, path := range paths[:min(limit, len(paths))] {
			att, err := b.upload(ctx, s.RemoteID, path)
			if err != nil {
				result.errors = append(result.errors, err.Error())
				continue
			}
			result.items = append(result.items, att)
		}
		return result, nil
	}, func(v any) {
		result := v.(uploadResult)
		s.Attachments = append(s.Attachments, result.items...)
		if len(result.errors) > 0 {
			a.Live.Error = strings.Join(result.errors, "\n")
		}
	})
}
func (a *preview) startVoice() {
	l := a.Live
	if l.Loading["voice"] {
		return
	}
	if l.Voice == nil {
		if !l.Connected || a.current().Archived {
			return
		}
		owner, generation := a.current(), l.VoiceGeneration
		a.run("voice", func(context.Context, *backendClient) (any, error) { return startMicrophone() }, func(v any) {
			l.Loading["voice"] = false
			recorder, _ := v.(voiceRecorder)
			if generation != l.VoiceGeneration {
				if recorder != nil {
					go recorder.Stop()
				}
				return
			}
			l.Voice = recorder
			l.VoiceOwner = owner
			l.VoiceStarted = time.Now()
			l.Notice = "正在录音，再次点击麦克风结束并转写。"
		})
		return
	}
	voice := l.Voice
	l.Voice = nil
	l.Notice = "正在转写语音…"
	s, generation := l.VoiceOwner, l.VoiceGeneration
	l.VoiceOwner = nil
	a.run("voice", func(ctx context.Context, b *backendClient) (any, error) {
		data, err := voice.Stop()
		if err != nil {
			return nil, err
		}
		if b == nil {
			return nil, errors.New("尚未连接运行主机")
		}
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, err := w.CreateFormFile("file", "voice.wav")
		if err != nil {
			return nil, err
		}
		_, _ = part.Write(data)
		_ = w.Close()
		req, err := http.NewRequestWithContext(ctx, "POST", b.URL+"/asr/transcribe", &body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		var v map[string]any
		err = b.execute(req, &v)
		return v, err
	}, func(v any) {
		l.Loading["voice"] = false
		if generation != l.VoiceGeneration || s == nil {
			return
		}
		for _, current := range a.Sessions {
			if current.ID == s.ID {
				s = current
				break
			}
		}
		l.Notice = "语音转写完成。"
		s.Draft += str(object(v)["text"])
		if a.current() == s {
			a.ReturnToComposer = true
		}
	})
}

func (a *preview) cancelVoice() {
	if a.Live == nil {
		return
	}
	l := a.Live
	l.VoiceGeneration++
	if l.Voice != nil {
		recorder := l.Voice
		l.Voice = nil
		go recorder.Stop()
	}
	l.VoiceOwner = nil
}
