package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type liveResult struct {
	epoch   uint64
	apply   func()
	discard func()
}
type liveState struct {
	Market                                                                 nativeMarketState
	CollapsedProjects                                                      map[string]bool
	ModelIndex                                                             int
	CommandIndex                                                           int
	RenameOwner                                                            *session
	EditQueueOwner                                                         *session
	EditQueueID, QueueDraft                                                string
	Dir                                                                    string
	Config                                                                 clientConfig
	Secrets                                                                secretStore
	Core                                                                   *localBackend
	Client                                                                 *backendClient
	ctx                                                                    context.Context
	hostCtx                                                                context.Context
	Store                                                                  *nativeStore
	cancel                                                                 context.CancelFunc
	streamCancel                                                           context.CancelFunc
	epoch                                                                  uint64
	results                                                                chan liveResult
	Connected, Connecting                                                  bool
	Notice, Error                                                          string
	Models                                                                 []string
	ResourceErrors                                                         map[string]string
	Data                                                                   map[string]any
	Loading                                                                map[string]bool
	Versions                                                               map[string]uint64
	DraftInputs                                                            map[string]draftInputState
	LastDraftInputs                                                        string
	Drafts                                                                 map[string]string
	LastFileState                                                          string
	LastDrafts                                                             string
	LastSave                                                               time.Time
	Rename, DeleteSession, NewWorkspace                                    bool
	TitleInput                                                             string
	ResourceTab                                                            string
	Workbench                                                              nativeWorkbench
	QRCode, QRURL, QRUserCode                                              string
	QRImage                                                                []byte
	QRBitmap                                                               *ui.Bitmap
	Voice                                                                  voiceRecorder
	VoiceOwner                                                             *session
	VoiceStarted                                                           time.Time
	VoiceGeneration                                                        uint64
	QRGeneration                                                           uint64
	LoginCancel                                                            context.CancelFunc
	LoginGeneration                                                        uint64
	ImagePreview                                                           ui.ImageSource
	ImageTitle                                                             string
	ImageZoom                                                              float32
	Images                                                                 map[string]ui.ImageSource
	Music                                                                  nativeMusic
	WorkspacePath, WorkspaceHost                                           string
	FolderButton                                                           ui.Handle
	PickDirectory                                                          func(mygo.OpenDialogOptions) ([]string, error)
	QRExpires, QRNext                                                      time.Time
	QRInterval                                                             time.Duration
	QRAuthorized                                                           bool
	LoginName, LoginScope, LoginWorkspace, LoginState, LoginCode, LoginURL string
	Confirm                                                                *nativeConfirmation
}
type nativeConfirmation struct {
	Title, Description string
	Action             func()
}

func newConnectedPreview(dir string) *preview {
	a := newPreview()
	ctx, cancel := context.WithCancel(context.Background())
	l := &liveState{Dir: dir, Secrets: systemSecrets{}, Core: &localBackend{}, ctx: ctx, cancel: cancel, epoch: 1, results: make(chan liveResult, 1024), Models: []string{}, ResourceErrors: map[string]string{}, Data: map[string]any{}, Loading: map[string]bool{}, Versions: map[string]uint64{}, Drafts: map[string]string{}, DraftInputs: map[string]draftInputState{}, CollapsedProjects: map[string]bool{}, Workbench: newNativeWorkbench()}
	l.Config = clientConfig{Hosts: []hostConfig{{ID: "local", Name: "此 Mac", Local: true}}, ActiveHost: "local", Model: modelConfig{Mode: "inherit"}}
	l.hostCtx = ctx
	l.Store = &nativeStore{dir: dir}
	l.Images = map[string]ui.ImageSource{}
	if data, err := os.ReadFile(filepath.Join(dir, "connections.json")); err == nil {
		if json.Unmarshal(data, &l.Config) != nil {
			l.Error = "连接配置无法读取，已保留原文件"
		}
	}
	if len(l.Config.Hosts) == 0 {
		l.Config.Hosts = []hostConfig{{ID: "local", Name: "此 Mac", Local: true}}
	}
	if l.Config.Model.Mode == "" {
		l.Config.Model.Mode = "inherit"
	}
	if data, err := os.ReadFile(filepath.Join(dir, "drafts.json")); err == nil {
		_ = json.Unmarshal(data, &l.Drafts)
	}
	a.Live = l
	a.loadFileState()
	a.loadDraftInputs()
	a.Sessions = []*session{{ID: 1, Title: "新会话", Model: l.Config.Model.Model, Workspace: l.workspace(), Project: filepath.Base(l.workspace())}}
	a.Active = 1
	a.restoreDraftInputs(a.current())
	a.NextID = 2
	a.Settings.loadPreferences(filepath.Join(dir, "native-preview-preferences.json"))
	a.syncSettingsConfig()
	return a
}
func (a *preview) syncSettingsConfig() {
	l := a.Live
	s := &a.Settings
	s.Hosts = nil
	for i, h := range l.Config.Hosts {
		s.Hosts = append(s.Hosts, demoHost{h.ID, h.Name, h.URL, h.Local})
		if h.ID == l.Config.ActiveHost {
			s.Host = i
		}
	}
	m := l.Config.Model
	s.Mode = "沿用命令行配置"
	if m.Mode == "override" {
		s.Mode = "在桌面客户端配置"
	}
	s.Provider = "OpenAI 兼容服务 / 网关"
	if m.Provider == "anthropic" {
		s.Provider = "Anthropic"
	}
	s.BaseURL = m.BaseURL
	s.Model = m.Model
	s.HasKey = m.HasKey
	s.Workspace = l.workspace()
}
func (a *preview) post(epoch uint64, fn func()) {
	a.postOwned(epoch, fn, nil)
}
func (a *preview) postOwned(epoch uint64, fn, discard func()) {
	if a.Live.ctx.Err() != nil {
		if discard != nil {
			discard()
		}
		return
	}
	select {
	case a.Live.results <- liveResult{epoch, fn, discard}:
	case <-a.Live.ctx.Done():
		if discard != nil {
			discard()
		}
	}
}
func (a *preview) liveTick() {
	l := a.Live
	for i := 0; i < 256; i++ {
		select {
		case r := <-l.results:
			if r.epoch == l.epoch {
				r.apply()
			} else if r.discard != nil {
				go r.discard()
			}
		default:
			i = 256
		}
	}
	if (l.Workbench.Term != nil || l.Workbench.TerminalStarting) && !l.Workbench.matchesTerminal(l.Config.ActiveHost, a.current().RemoteID) {
		l.Workbench.close()
	}
	if time.Since(l.LastSave) > 750*time.Millisecond {
		a.saveFileState()
		a.saveDraftInputs()
		for _, s := range a.Sessions {
			l.Drafts[l.Config.ActiveHost+"/"+s.RemoteID] = s.Draft
		}
		data, _ := json.Marshal(l.Drafts)
		if string(data) != l.LastDrafts && !l.Loading["draft-save"] {
			snapshot := map[string]string{}
			for k, v := range l.Drafts {
				snapshot[k] = v
			}
			version := l.Store.reserve("drafts.json")
			serialized := string(data)
			a.run("draft-save", func(context.Context, *backendClient) (any, error) {
				return nil, l.Store.save("drafts.json", version, snapshot)
			}, func(any) { l.LastDrafts = serialized })
		}
		l.LastSave = time.Now()
	}
	if a.Settings.Open && a.Settings.Tab == "feishu" && l.QRCode != "" && !l.QRAuthorized && time.Now().Before(l.QRExpires) && time.Now().After(l.QRNext) && !l.Loading["qr-poll"] {
		l.QRNext = time.Now().Add(l.QRInterval)
		a.pollQR()
	}
}
func (a *preview) background(work func(context.Context, *backendClient) (any, error), done func(any)) {
	a.run("", work, done)
}
func (a *preview) run(key string, work func(context.Context, *backendClient) (any, error), done func(any)) {
	l := a.Live
	epoch, ctx, client := l.epoch, l.hostCtx, l.Client
	if key != "" {
		if l.Loading[key] {
			return
		}
		l.Loading[key] = true
	}
	go func() {
		value, err := work(ctx, client)
		var cleanup func()
		if voice, ok := value.(voiceRecorder); ok {
			cleanup = func() { _, _ = voice.Stop() }
		} else if closer, ok := value.(interface{ Close() }); ok {
			cleanup = closer.Close
		}
		a.postOwned(epoch, func() {
			if key != "" {
				l.Loading[key] = false
			}
			if err != nil {
				l.Error = err.Error()
				return
			}
			if done != nil {
				done(value)
			}
		}, cleanup)
	}()
}
func (a *preview) call(method, path string, body any, done func(any)) {
	a.background(func(ctx context.Context, b *backendClient) (any, error) {
		if b == nil {
			return nil, errors.New("尚未连接运行主机")
		}
		var v any
		err := b.request(ctx, method, path, body, &v)
		return v, err
	}, done)
}
func (a *preview) resource(key, path string) {
	l := a.Live
	l.Versions[key]++
	version := l.Versions[key]
	l.Loading[key] = true
	delete(l.ResourceErrors, key)
	epoch, ctx, b := l.epoch, l.hostCtx, l.Client
	go func() {
		var value any
		var err error
		if b == nil {
			err = errors.New("尚未连接运行主机")
		} else {
			err = b.request(ctx, "GET", path, nil, &value)
		}
		a.post(epoch, func() {
			if l.Versions[key] != version {
				return
			}
			l.Loading[key] = false
			if err != nil {
				l.Error = err.Error()
				l.ResourceErrors[key] = err.Error()
				return
			}
			l.Data[key] = value
		})
	}()
}
func (a *preview) connectLive() {
	l := a.Live
	a.cancelQR()
	a.cancelMCPLogin()
	a.cancelVoice()
	if l.configBusy() {
		l.Error = "正在保存配置，请稍后重连"
		return
	}
	if l.streamCancel != nil {
		l.streamCancel()
	}
	l.Workbench.close()
	if l.Music.Player != nil {
		l.Music.Player.Close()
		l.Music.Player = nil
	}
	if l.Voice != nil {
		voice := l.Voice
		l.Voice = nil
		go voice.Stop()
	}
	l.epoch++
	l.Market = nativeMarketState{}
	l.NewWorkspace = false
	a.NewMenu = false
	a.Settings.SkillDetail = ""
	for _, key := range []string{"market", "installed", "sections", "skill-detail"} {
		delete(l.Data, key)
	}
	l.Loading = map[string]bool{}
	epoch := l.epoch
	l.Connecting = true
	l.Connected = false
	l.Error = ""
	l.Client = nil
	ctx, cancel := context.WithCancel(l.ctx)
	l.streamCancel = cancel
	l.hostCtx = ctx
	h := l.Config.Hosts[a.Settings.Host]
	cfg, dir, secrets := l.Config.Model, l.Dir, l.Secrets
	go func() {
		var b *backendClient
		var err error
		if h.Local {
			b, err = l.Core.start(ctx, dir, cfg, secrets)
		} else {
			var token string
			if h.HasToken {
				token, err = secrets.Get("host/" + h.ID)
			}
			if err == nil {
				b, err = newBackendClient(h.URL, token)
			}
			if err == nil {
				err = b.request(ctx, "GET", "/health", nil, nil)
			}
		}
		if err != nil {
			a.post(epoch, func() { l.Connecting = false; l.Error = err.Error() })
			return
		}
		b, _ = newBackendClient(b.URL, b.Token)
		a.post(epoch, func() {
			l.Client = b
			l.Connecting = false
			a.refreshLiveSessions("")
			a.refreshModels()
			if a.Settings.Open {
				a.loadSettingsPage()
			}
		})
		b.observe(ctx, func(e envelope) { a.post(epoch, func() { a.receive(e) }) }, func(connected bool) {
			a.post(epoch, func() {
				l.Connected = connected
				if connected {
					l.Error = ""
					for _, s := range a.Sessions {
						if s.RemoteID != "" {
							a.subscribe(s)
						}
					}
				} else {
					l.Error = "连接中断，草稿已保留；正在重连"
					for _, s := range a.Sessions {
						s.Sending = false
					}
				}
			})
		})
	}()
}
func (a *preview) receive(e envelope) {
	l := a.Live
	for _, s := range a.Sessions {
		if s.RemoteID != e.SessionID {
			continue
		}
		following := s.Scroll.Y >= s.Scroll.MaxY-12
		if e.Type == "accepted" && e.RequestID == s.PendingRequest || e.Type == "snapshot" && e.Run != nil && e.Run.RequestID == s.PendingRequest {
			if s.Draft == s.SentDraft {
				s.Draft = ""
				s.Attachments = nil
				s.FileReferences = nil
			}
			s.Sending = false
			s.PendingRequest = ""
		}
		if e.Type == "error" && e.RequestID == s.PendingRequest {
			s.Sending = false
			l.Error = e.Message
		}
		s.reduce(e)
		if following {
			follow(s)
		}
		if e.Type == "status" && !s.Running {
			a.refreshRunResults(s)
			if a.current() != s && s.LastNotifiedRun != s.runID() {
				s.Unread++
				s.LastNotifiedRun = s.runID()
			}
			a.refreshLiveSessions("")
			if a.current() == s && a.Workbench && a.Live.Workbench.Tab == "review" {
				a.refreshWorkbench()
			}
		}
		return
	}
	if e.Type == "error" {
		l.Error = e.Message
	}
}
func (a *preview) subscribe(s *session) {
	if a.Live.Client == nil || s.RemoteID == "" {
		return
	}
	b := a.Live.Client
	value := map[string]any{"type": "subscribe", "session_id": s.RemoteID, "after_seq": s.Seq, "run_id": s.runID()}
	a.background(func(context.Context, *backendClient) (any, error) { return nil, b.send(value) }, nil)
}
func (a *preview) refreshModels() {
	a.operation("model-list", "GET", "/models", nil, func(v any) {
		m := object(v)
		a.Live.Data["models"] = v
		var models []string
		for _, item := range list(m["models"]) {
			id := str(object(item)["id"])
			if id != "" {
				models = append(models, id)
			}
		}
		a.Live.Models = models
		if model := str(object(m["current"])["id"]); a.current().Model == "" {
			a.current().Model = model
		}
	})
}
func (a *preview) refreshLiveSessions(prefer string) {
	l := a.Live
	l.Versions["sessions"]++
	version := l.Versions["sessions"]
	a.call("GET", "/sessions", nil, func(v any) {
		if version != l.Versions["sessions"] {
			return
		}
		old := map[string]*session{}
		for _, s := range a.Sessions {
			old[s.RemoteID] = s
		}
		current := a.current().RemoteID
		var next []*session
		for _, item := range list(v) {
			m := object(item)
			id := str(m["id"])
			if id == "" {
				continue
			}
			s := old[id]
			if s == nil {
				s = &session{ID: a.NextID, RemoteID: id, Draft: l.Drafts[l.Config.ActiveHost+"/"+id]}
				a.NextID++
				a.restoreDraftInputs(s)
			}
			s.Title = str(m["title"])
			s.UpdatedAt = str(m["updated_at"])
			if s.Title == "" {
				s.Title = "会话 " + id[max(0, len(id)-6):]
			}
			if path := str(m["workspace"]); path != "" {
				s.Workspace = path
			}
			s.Project = filepath.Base(s.Workspace)
			s.Application = str(m["application"])
			s.Archived = boolean(m["archived"])
			s.Pinned = boolean(m["pinned"])
			next = append(next, s)
		}
		sort.SliceStable(next, func(i, j int) bool { return next[i].Pinned && !next[j].Pinned })
		if len(next) == 0 {
			next = []*session{old[""]}
			if next[0] == nil {
				next[0] = &session{ID: a.NextID, Title: "新会话", Workspace: l.workspace(), Draft: l.Drafts[l.Config.ActiveHost+"/"]}
				a.NextID++
			}
		}
		a.Sessions = next
		chosen := next[0]
		for _, s := range next {
			if s.RemoteID == current {
				chosen = s
			}
		}
		for _, s := range next {
			if prefer != "" && s.RemoteID == prefer {
				chosen = s
			}
		}
		changed := a.Active != chosen.ID
		a.Active = chosen.ID
		if changed || chosen.Seq == 0 {
			a.loadSession(chosen)
		}
	})
}
func (a *preview) loadSession(s *session) {
	if s.RemoteID == "" || a.Live.Client == nil {
		return
	}
	id := s.RemoteID
	a.refreshRunResults(s)
	a.call("GET", sessionPath(id)+"/run", nil, func(v any) {
		data, _ := json.Marshal(v)
		var e envelope
		_ = json.Unmarshal(data, &e)
		a.receive(e)
		a.subscribe(s)
	})
	s.InfoVersion++
	infoVersion := s.InfoVersion
	a.call("GET", sessionPath(id)+"/info", nil, func(v any) {
		if s.InfoVersion != infoVersion {
			return
		}
		m := object(v)
		s.Workspace = str(m["workspace"])
		s.Project = filepath.Base(s.Workspace)
		s.Model = str(m["model"])
		s.Usage = object(m["context_usage"])
		s.AccessMode = str(m["access_mode"])
		if a.current() == s {
			a.Settings.Workspace = s.Workspace
			if a.Workbench {
				a.refreshWorkbench()
			}
		}
	})
}
func (a *preview) createLiveSession(draft string) {
	workspace := a.current().Workspace
	if workspace == "" {
		workspace = a.Live.workspace()
	}
	a.createLiveSessionIn(draft, workspace, "")
}
func (a *preview) createLiveSessionIn(draft, workspace, application string, ready ...func(*session)) {
	if a.Live.configBusy() {
		a.Live.Error = "正在应用连接配置，请稍后创建会话"
		return
	}
	if a.Live.Loading["create-session"] {
		return
	}

	model := a.current().Model
	origin := a.current()
	initial := origin.RemoteID == ""
	a.Live.Versions["sessions"]++
	a.operation("create-session", "POST", "/sessions", map[string]any{"cwd": workspace, "model": model, "application": application}, func(v any) {
		id := str(object(v)["id"])
		if id == "" {
			return
		}
		a.Live.Versions["sessions"]++
		s := &session{ID: a.NextID, RemoteID: id, Title: "新会话", Workspace: workspace, Project: filepath.Base(workspace), Model: model, Draft: draft, Application: application}
		a.NextID++
		if initial {
			s.ID = origin.ID
			s.Draft = origin.Draft
			s.Attachments = origin.Attachments
			s.FileReferences = origin.FileReferences
			for i, item := range a.Sessions {
				if item == origin {
					a.Sessions[i] = s
				}
			}
		} else {
			a.Sessions = append([]*session{s}, a.Sessions...)
		}
		a.Active = s.ID
		a.Archived = false
		a.Search = ""
		a.loadSession(s)
		a.ReturnToComposer = true
		if len(ready) > 0 {
			ready[0](s)
		}
		if draft != "" && s.Draft == draft {
			a.submitLive()
		}
	})
}
func (a *preview) submitLive() {
	s := a.current()
	l := a.Live
	text := strings.TrimSpace(s.Draft)
	if text == "" || s.Sending || s.Archived || l.Loading["queue-add/"+s.RemoteID] || l.Loading["attachments/"+s.RemoteID] || l.Loading["voice"] || l.Voice != nil {
		return
	}
	if l.configBusy() {
		l.Error = "正在应用连接配置，草稿已保留"
		return
	}
	if !l.Connected {
		l.Error = "尚未连接，草稿已保留"
		return
	}
	if s.RemoteID == "" {
		a.createLiveSession(s.Draft)
		return
	}
	if strings.HasPrefix(text, "/") && a.executeLiveCommand(text) {
		return
	}
	a.sendLivePrompt(s, text, s.Draft, s.Attachments)
}
func (a *preview) sendLivePrompt(s *session, text, original string, attachments []map[string]any, providedInputs ...map[string]any) {
	l := a.Live
	if !l.Connected || l.configBusy() || s.Sending || s.Archived {
		l.Error = "当前无法提交，草稿已保留"
		return
	}
	inputs := promptInputMap(s, attachments)
	if len(providedInputs) > 0 && providedInputs[0] != nil {
		inputs = providedInputs[0]
	}
	if s.Running || s.Pending {
		if s.PendingRequest == "" || s.SentDraft != original {
			s.PendingRequest = requestID()
		}
		s.SentDraft = original
		body := map[string]any{"action": "add", "id": s.PendingRequest, "prompt": text, "inputs": inputs}
		a.operation("queue-add/"+s.RemoteID, "POST", sessionPath(s.RemoteID)+"/queue", body, func(v any) {
			raw, _ := json.Marshal(v)
			_ = json.Unmarshal(raw, &s.Queue)
			if s.Draft == original {
				s.Draft = ""
				s.Attachments = nil
				s.FileReferences = nil
			}
			s.PendingRequest = ""
		})
		return
	}
	if s.PendingRequest == "" || s.SentDraft != original {
		s.PendingRequest = requestID()
	}
	s.SentDraft = original
	s.Sending = true
	request := s.PendingRequest
	value := map[string]any{"type": "prompt", "session_id": s.RemoteID, "request_id": request, "prompt": text, "inputs": inputs}
	a.SlashMenu = false
	a.background(func(ctx context.Context, b *backendClient) (any, error) {
		if err := b.send(value); err != nil {
			return nil, err
		}
		return nil, nil
	}, nil)
	epoch := l.epoch
	go func() {
		select {
		case <-l.ctx.Done():
			return
		case <-time.After(12 * time.Second):
		}
		a.post(epoch, func() {
			if s.Sending && s.PendingRequest == request {
				s.Sending = false
				l.Error = "服务尚未确认接收，草稿已保留；可重试，重复请求不会重复执行"
			}
		})
	}()
}
func (a *preview) cancelLive() {
	s := a.current()
	a.call("POST", sessionPath(s.RemoteID)+"/run/cancel", map[string]any{"run_id": s.runID()}, func(any) { a.loadSession(s) })
}
func (a *preview) confirmLive(approved bool) {
	s := a.current()
	if len(s.Confirmations) == 0 {
		return
	}
	id := s.Confirmations[0].ID
	a.confirmSession(s, id, approved, nil)
}
func (a *preview) forkLive(index int) {
	s := a.current()
	if s.Running || index >= len(s.Messages) || s.Messages[index].EntryID == "" {
		a.Live.Error = "这条回复尚无可用的分支点"
		return
	}
	a.call("POST", sessionPath(s.RemoteID)+"/fork", map[string]any{"entry_id": s.Messages[index].EntryID}, func(v any) { a.refreshLiveSessions(str(object(v)["id"])) })
}
func (a *preview) forkUserMessage(index int) {
	s := a.current()
	if index < 0 || index >= len(s.Messages) || !s.Messages[index].User || s.Running {
		return
	}
	before := 0
	for _, m := range s.Messages[:index] {
		if m.User {
			before++
		}
	}
	text := s.Messages[index].Text
	host := a.Live.Config.ActiveHost
	a.operation("fork-user", "POST", sessionPath(s.RemoteID)+"/fork", map[string]any{"before_message_index": before}, func(v any) { id := str(object(v)["id"]); a.Live.Drafts[host+"/"+id] = text; a.refreshLiveSessions(id) })
}
func (a *preview) patchLiveSession(patch map[string]any) {
	a.patchSessionPreference(a.current(), patch, func() { a.Live.Rename = false; a.MoreMenu = false })
}
func (a *preview) switchLiveModel(name string) {
	s := a.current()
	if s.RemoteID == "" {
		s.Model = name
		a.ModelMenu = false
		return
	}
	s.InfoVersion++
	a.operation("model/"+s.RemoteID, "POST", sessionPath(s.RemoteID)+"/model", map[string]any{"model": name}, func(v any) {
		s.InfoVersion++
		s.Model = str(object(v)["model"])
		a.ModelMenu = false
		if a.current() == s {
			a.ReturnToComposer = true
		}
	})
}
func (a *preview) switchLivePermission(mode string) {
	s := a.current()
	if s.RemoteID == "" {
		a.Live.Error = "请先创建会话"
		return
	}
	key := "ask"
	if mode == "完全权限" {
		key = "full"
	}
	s.InfoVersion++
	a.operation("permission/"+s.RemoteID, "POST", sessionPath(s.RemoteID)+"/permissions", map[string]any{"mode": key}, func(any) {
		s.InfoVersion++
		s.AccessMode = key
		if a.current() == s {
			a.PermissionMenu = false
			a.ReturnToComposer = true
		}
	})
}
func (a *preview) closeLive() {
	if a.Live == nil {
		return
	}
	l := a.Live
	if l.Voice != nil {
		_, _ = l.Voice.Stop()
	}
	l.cancel()
	if l.streamCancel != nil {
		l.streamCancel()
	}
	l.Workbench.close()
	l.Core.close()
	if l.Music.Player != nil {
		l.Music.Player.Close()
		l.Music.Player = nil
	}
	for {
		select {
		case r := <-l.results:
			if r.discard != nil {
				r.discard()
			}
		default:
			goto drained
		}
	}
drained:
	for _, s := range a.Sessions {
		l.Drafts[l.Config.ActiveHost+"/"+s.RemoteID] = s.Draft
	}
	version := l.Store.reserve("drafts.json")
	_ = l.Store.save("drafts.json", version, l.Drafts)
	version = l.Store.reserve("workspace-files.json")
	_ = l.Store.save("workspace-files.json", version, a.fileStateSnapshot())
	version = l.Store.reserve("draft-inputs.json")
	_ = l.Store.save("draft-inputs.json", version, a.captureDraftInputs())
}

func (a *preview) livePermission() string {
	if a.current().AccessMode == "full" {
		return "完全权限"
	}
	return "每次询问"
}

// A local project path must never be reused automatically on another host.
func (l *liveState) workspace() string {
	for _, h := range l.Config.Hosts {
		if h.ID == l.Config.ActiveHost {
			if h.Workspace != "" {
				return h.Workspace
			}
			if h.Local {
				return l.Config.Workspace
			}
			return ""
		}
	}
	return ""
}
