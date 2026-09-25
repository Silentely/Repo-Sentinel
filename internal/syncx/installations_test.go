package syncx

import (
	"context"
	"errors"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type failingRepositoryList struct {
	store.RepositoryStore
	err error
}

func (s failingRepositoryList) List(context.Context, store.ListFilter) ([]store.Repository, store.PageResult, error) {
	return nil, store.PageResult{}, s.err
}

func TestLoadExistingRepositoriesStopsOnListFailure(t *testing.T) {
	want := errors.New("repository list unavailable")
	_, err := loadExistingRepositories(context.Background(), failingRepositoryList{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("loadExistingRepositories error = %v, want %v", err, want)
	}
}
