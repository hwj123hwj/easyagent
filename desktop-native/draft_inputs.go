package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

type draftInputState struct {
	Attachments []map[string]any `json:"attachments"`
	Files       []fileReference  `json:"files"`
}

func (a *preview) loadDraftInputs() {
	var entries map[string]draftInputState
	if data, err := os.ReadFile(filepath.Join(a.Live.Dir, "draft-inputs.json")); err == nil && json.Unmarshal(data, &entries) == nil && entries != nil {
		a.Live.DraftInputs = entries
	}
}
func (a *preview) restoreDraftInputs(s *session) {
	value := a.Live.DraftInputs[a.Live.Config.ActiveHost+"/"+s.RemoteID]
	s.Attachments = append([]map[string]any{}, value.Attachments[:min(8, len(value.Attachments))]...)
	s.FileReferences = append([]fileReference{}, value.Files[:min(12, len(value.Files))]...)
	for i := range s.FileReferences {
		s.FileReferences[i].Host = a.Live.Config.ActiveHost
	}
}
func (a *preview) captureDraftInputs() json.RawMessage {
	l := a.Live
	for _, s := range a.Sessions {
		l.DraftInputs[l.Config.ActiveHost+"/"+s.RemoteID] = draftInputState{Attachments: s.Attachments, Files: s.FileReferences}
	}
	data, _ := json.Marshal(l.DraftInputs)
	return data
}
func (a *preview) saveDraftInputs() {
	l := a.Live
	data := a.captureDraftInputs()
	if string(data) == l.LastDraftInputs || l.Loading["draft-inputs-save"] {
		return
	}
	version := l.Store.reserve("draft-inputs.json")
	a.run("draft-inputs-save", func(context.Context, *backendClient) (any, error) {
		return nil, l.Store.save("draft-inputs.json", version, data)
	}, func(any) { l.LastDraftInputs = string(data) })
}
