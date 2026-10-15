package session

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func storageWithLongPath(n int) *JSONLStorage {
	s := NewJSONLStorage("")
	parent := ""
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("entry-%d", i)
		s.byID[id] = Entry{ID: id, Type: EntryTypeMessage, ParentID: parent}
		parent = id
	}
	s.leafID = parent
	return s
}

func TestJSONLStorageLongPathOrder(t *testing.T) {
	storage := storageWithLongPath(2000)
	path, err := storage.GetPathToRoot(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, path, 2000)
	for i, entry := range path {
		require.Equal(t, fmt.Sprintf("entry-%d", i), entry.ID)
	}
	// 线性收集只需 slice 增长；逐项 prepend 的旧实现每个 entry 都分配一次。
	allocs := testing.AllocsPerRun(1, func() {
		_, err := storage.GetPathToRoot(context.Background(), "")
		require.NoError(t, err)
	})
	require.Less(t, allocs, float64(100))
}

func BenchmarkJSONLStorageGetPathToRoot(b *testing.B) {
	for _, n := range []int{500, 1000, 2000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			storage := storageWithLongPath(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := storage.GetPathToRoot(context.Background(), "")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
