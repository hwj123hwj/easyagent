//go:build darwin

package main

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type audioFormat struct {
	Rate                                                                           float64
	Format, Flags, PacketBytes, PacketFrames, FrameBytes, Channels, Bits, Reserved uint32
}
type audioBuffer struct {
	Capacity uint32
	Data     uintptr
	Size     uint32
	User     uintptr
}
type microphone struct {
	mu      sync.Mutex
	queue   uintptr
	pcm     []byte
	stopped bool
	token   uintptr
}

var audioAPI struct {
	once      sync.Once
	err       error
	newInput  func(*audioFormat, uintptr, uintptr, uintptr, uintptr, uint32, *uintptr) int32
	allocate  func(uintptr, uint32, *uintptr) int32
	enqueue   func(uintptr, uintptr, uint32, uintptr) int32
	start     func(uintptr, uintptr) int32
	stop      func(uintptr, bool) int32
	dispose   func(uintptr, bool) int32
	callback  uintptr
	mu        sync.Mutex
	next      uintptr
	recorders map[uintptr]*microphone
}

func startMicrophone() (voiceRecorder, error) {
	audioAPI.once.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_NOW|purego.RTLD_LOCAL)
		audioAPI.err = err
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&audioAPI.newInput, lib, "AudioQueueNewInput")
		purego.RegisterLibFunc(&audioAPI.allocate, lib, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&audioAPI.enqueue, lib, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&audioAPI.start, lib, "AudioQueueStart")
		purego.RegisterLibFunc(&audioAPI.stop, lib, "AudioQueueStop")
		purego.RegisterLibFunc(&audioAPI.dispose, lib, "AudioQueueDispose")
		audioAPI.recorders = map[uintptr]*microphone{}
		audioAPI.callback = purego.NewCallback(func(token, queue, buffer, startTime uintptr, packets uint32, descriptions uintptr) {
			audioAPI.mu.Lock()
			m := audioAPI.recorders[token]
			audioAPI.mu.Unlock()
			if m == nil {
				return
			}
			buf := (*audioBuffer)(nativePointer(buffer))
			m.mu.Lock()
			if m.stopped {
				m.mu.Unlock()
				return
			}
			if buf.Data != 0 && buf.Size > 0 && buf.Size <= buf.Capacity && len(m.pcm)+int(buf.Size) <= 20<<20 {
				m.pcm = append(m.pcm, unsafe.Slice((*byte)(nativePointer(buf.Data)), int(buf.Size))...)
			}
			m.mu.Unlock()
			_ = audioAPI.enqueue(queue, buffer, 0, 0)
		})
	})
	if audioAPI.err != nil {
		return nil, errors.New("音频录制不可用")
	}
	m := &microphone{}
	audioAPI.mu.Lock()
	audioAPI.next++
	m.token = audioAPI.next
	audioAPI.recorders[m.token] = m
	audioAPI.mu.Unlock()
	format := audioFormat{Rate: 16000, Format: 0x6c70636d, Flags: 12, PacketBytes: 2, PacketFrames: 1, FrameBytes: 2, Channels: 1, Bits: 16}
	if audioAPI.newInput(&format, audioAPI.callback, m.token, 0, 0, 0, &m.queue) != 0 {
		audioAPI.mu.Lock()
		delete(audioAPI.recorders, m.token)
		audioAPI.mu.Unlock()
		return nil, errors.New("无法打开麦克风，请检查系统麦克风权限")
	}
	for i := 0; i < 3; i++ {
		var buffer uintptr
		if audioAPI.allocate(m.queue, 8192, &buffer) != 0 || audioAPI.enqueue(m.queue, buffer, 0, 0) != 0 {
			_, _ = m.Stop()
			return nil, errors.New("初始化录音缓冲区失败")
		}
	}
	if audioAPI.start(m.queue, 0) != 0 {
		_, _ = m.Stop()
		return nil, errors.New("录音启动失败，请检查麦克风权限")
	}
	return m, nil
}
func (m *microphone) Stop() ([]byte, error) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil, errors.New("录音已结束")
	}
	m.stopped = true
	m.mu.Unlock()
	_ = audioAPI.stop(m.queue, true)
	_ = audioAPI.dispose(m.queue, true)
	audioAPI.mu.Lock()
	delete(audioAPI.recorders, m.token)
	audioAPI.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pcm) == 0 {
		return nil, errors.New("没有录到声音，请检查麦克风权限")
	}
	return pcmWAV(m.pcm), nil
}
