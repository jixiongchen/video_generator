package store

import (
	"testing"
	"time"

	"video-generator/services/api/internal/model"
)

func TestPutGetAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Generation{ID: "gen-1", Status: "queued", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.Put(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(want.ID)
	if err != nil || got.Status != want.Status {
		t.Fatalf("got=%+v err=%v", got, err)
	}

	reloaded, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err = reloaded.Get(want.ID)
	if err != nil || got.ID != want.ID {
		t.Fatalf("reload got=%+v err=%v", got, err)
	}
}
