package session

import (
	"context"
	"os"
	"testing"

	"github.com/hwj123hwj/easyagent/sdk/ai"
	"github.com/stretchr/testify/require"
)

func TestJSONLStorageSyncFailureRejectsMessageAndCursor(t *testing.T) {
	for _, cursor := range []bool{false, true} {
		name := "tool-call-message"
		if cursor {
			name = "cursor"
		}
		t.Run(name, func(t *testing.T) {
			// Pipe writes succeed, while fsync is unsupported. This exercises
			// the durability failure after bytes were accepted by the OS.
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			defer reader.Close()
			defer writer.Close()
			storage := &JSONLStorage{file: writer, leafID: "root", byID: map[string]Entry{"root": {ID: "root", Type: EntryTypeMessage}}}
			if cursor {
				err = storage.SetLeaf(context.Background(), "new-root")
			} else {
				sess := New(storage)
				require.NoError(t, sess.InitFromStorage(context.Background()))
				err = sess.AppendMessage(context.Background(), ai.AssistantMessage{ToolCalls: []ai.ToolCall{{ID: "call", Name: "write", Args: "{}"}}})
			}
			require.Error(t, err, "failed fsync cannot acknowledge a durable tool declaration or branch change")
			require.Equal(t, "root", storage.leafID)
			require.Len(t, storage.byID, 1, "an unconfirmed append must not become visible in the in-memory index")
		})
	}
}
