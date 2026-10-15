package dynamicflow

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type wireMessage struct {
	Type, ID, Op, Key string
	Args              json.RawMessage
	State             json.RawMessage
	Result            json.RawMessage
	Error             string
}

func (m *Manager) execute(ctx context.Context, id string) error {
	parentCtx := ctx
	ctx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()
	run, _ := m.Get(id)
	cmd := exec.Command("node", "--max-old-space-size=384", m.runtime)
	// The script runtime has no reason to inherit provider keys or shell preload flags.
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SystemRoot"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// Never forward worker stderr: provider content and generated scripts can contain private data.
	if err = cmd.Start(); err != nil {
		return err
	}
	var writeMu sync.Mutex
	send := func(v any) error { writeMu.Lock(); defer writeMu.Unlock(); return json.NewEncoder(stdin).Encode(v) }
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = send(map[string]any{"type": "cancel"})
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				killProcess(cmd)
			}
		case <-finished:
		}
	}()
	_ = send(map[string]any{"type": "start", "run_id": id, "name": run.Name, "script": run.Script, "directory": filepath.Join(m.dir, id)})
	var requests sync.WaitGroup
	var askMu sync.Mutex
	cancels := map[string]context.CancelFunc{}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var failure error
	settled := false
	for scanner.Scan() {
		var msg wireMessage
		if err = json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			failure = errors.New("invalid workflow protocol")
			break
		}
		switch msg.Type {
		case "snapshot":
			m.mu.Lock()
			m.runs[id].State = msg.State
			err = m.save(m.runs[id])
			m.mu.Unlock()
			if err != nil {
				failure = err
			}
		case "fatal":
			failure = errors.New(msg.Error)
		case "settled":
			var result struct {
				Status   string          `json:"status"`
				Artifact json.RawMessage `json:"artifact"`
				Error    json.RawMessage `json:"error"`
			}
			if err = json.Unmarshal(msg.Result, &result); err != nil {
				failure = err
				break
			}
			m.mu.Lock()
			r := m.runs[id]
			r.Status = result.Status
			if result.Status == "errored" {
				r.Status = "failed"
			}
			r.Result = result.Artifact
			if result.Status != "completed" {
				r.Error = string(result.Error)
				if result.Status == "stopped" {
					r.Status = "cancelled"
				}
			}
			m.mu.Unlock()
			settled = true
		case "cancel_ask":
			askMu.Lock()
			if cancel := cancels[msg.Key]; cancel != nil {
				cancel()
			}
			askMu.Unlock()
		case "rpc":
			var args struct {
				Key          string          `json:"key"`
				Persona      Persona         `json:"persona"`
				SessionID    string          `json:"session_id"`
				Instructions string          `json:"instructions"`
				Typed        bool            `json:"typed"`
				Schema       json.RawMessage `json:"schema"`
			}
			if err = json.Unmarshal(msg.Args, &args); err != nil {
				failure = err
				break
			}
			requestCtx, cancel := context.WithCancel(ctx)
			if msg.Op == "ask" {
				askMu.Lock()
				cancels[args.Key] = cancel
				askMu.Unlock()
			}
			requests.Add(1)
			go func(msg wireMessage) {
				defer requests.Done()
				defer cancel()
				var result any
				var rpcErr error
				switch msg.Op {
				case "actor":
					m.mu.Lock()
					sessionID := m.runs[id].Actors[args.Key]
					m.mu.Unlock()
					sessionID, rpcErr = m.host.Actor(requestCtx, run.Parent, run.ActorConfig, args.Persona, sessionID)
					if rpcErr == nil {
						m.mu.Lock()
						m.runs[id].Actors[args.Key] = sessionID
						rpcErr = m.save(m.runs[id])
						m.mu.Unlock()
						result = map[string]string{"id": sessionID}
					}
				case "ask":
					// Worker can only address sessions created for this run, never arbitrary chats.
					m.mu.Lock()
					owned := false
					for _, sid := range m.runs[id].Actors {
						if sid == args.SessionID {
							owned = true
						}
					}
					m.mu.Unlock()
					if !owned {
						rpcErr = errors.New("unknown workflow actor")
						break
					}
					var answer string
					answer, rpcErr = m.host.Ask(requestCtx, args.SessionID, args.Instructions, args.Typed, args.Schema)
					result = map[string]string{"text": answer}
				default:
					rpcErr = fmt.Errorf("unsupported workflow operation %q", msg.Op)
				}
				// Cancellation is a run stop, never a cached failed ask.
				if ctx.Err() != nil {
					return
				}
				reply := map[string]any{"type": "response", "id": msg.ID, "result": result}
				if rpcErr != nil {
					reply["error"] = rpcErr.Error()
				}
				_ = send(reply)
			}(msg)
		}
		if failure != nil {
			break
		}
	}
	if failure == nil {
		failure = scanner.Err()
	}
	if failure != nil || !settled {
		killProcess(cmd)
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	askMu.Lock()
	for _, cancel := range cancels {
		cancel()
	}
	askMu.Unlock()
	cancelAll()
	requests.Wait()
	if failure != nil {
		return failure
	}
	if parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("workflow process failed: %w", waitErr)
	}
	if !settled {
		return errors.New("workflow process exited without settlement")
	}
	return nil
}
