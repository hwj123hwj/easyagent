package feishu

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OwnerAccess binds the first holder of a one-use, expiring pairing code.
// State lives outside the repository in a private file; codes are never logged.
type OwnerAccess struct {
	mu    sync.Mutex
	path  string
	state ownerState
}
type ownerState struct {
	Owner   string    `json:"owner,omitempty"`
	Code    string    `json:"code,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
}

func OpenOwnerAccess(path, owner string) (*OwnerAccess, error) {
	a := &OwnerAccess{path: path}
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &a.state); err != nil {
			return nil, fmt.Errorf("invalid owner state")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if owner != "" {
		a.state = ownerState{Owner: owner}
	}
	if a.state.Owner == "" && (a.state.Code == "" || time.Now().After(a.state.Expires)) {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		a.state = ownerState{Code: hex.EncodeToString(b), Expires: time.Now().Add(24 * time.Hour)}
	}
	if err := a.persist(a.state); err != nil {
		return nil, err
	}
	return a, nil
}
func (a *OwnerAccess) persist(s ownerState) error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.path), ".owner-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	b, _ := json.Marshal(s)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), a.path)
}
func (a *OwnerAccess) Allowed(id string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return id != "" && id == a.state.Owner
}

// Pair consumes only an exact text command in a private conversation.
func (a *OwnerAccess) Pair(msg Message) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Owner != "" || a.state.Code == "" || time.Now().After(a.state.Expires) || msg.SenderOpenID == "" || msg.ChatType != "p2p" || msg.MsgType != "text" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(msg.Text), []byte("/pair "+a.state.Code)) != 1 {
		return false
	}
	next := ownerState{Owner: msg.SenderOpenID}
	if a.persist(next) != nil {
		return false
	}
	a.state = next
	return true
}
func isPairCommand(text string) bool { return strings.HasPrefix(text, "/pair") }
