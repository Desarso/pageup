package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type storedObject struct {
	Body         []byte
	LastModified time.Time
}

// objectStore holds shared files and their metadata under slash-separated
// keys. Pages keep their original layout in the data directory.
type objectStore interface {
	Get(string) (storedObject, error)
	Put(string, []byte, bool) (bool, error)
	Delete(string) error
	// PutStream and OpenStream move hosted files without buffering them in
	// memory. OpenStream receives the size recorded in file metadata.
	PutStream(context.Context, string, io.ReadSeeker, int64, string) error
	OpenStream(context.Context, string, int64) (io.ReadSeekCloser, error)
}

type filesystemObjectStore struct {
	root string
}

func newFilesystemObjectStore(root string) (*filesystemObjectStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage directory cannot be empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	return &filesystemObjectStore{root: root}, nil
}

func (store *filesystemObjectStore) path(key string) string {
	return filepath.Join(store.root, filepath.FromSlash(key))
}

func (store *filesystemObjectStore) Get(key string) (storedObject, error) {
	path := store.path(key)
	body, err := os.ReadFile(path)
	if err != nil {
		return storedObject{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return storedObject{}, err
	}
	return storedObject{Body: body, LastModified: info.ModTime()}, nil
}

func (store *filesystemObjectStore) Put(key string, body []byte, createOnly bool) (bool, error) {
	path := store.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	if createOnly {
		return writeImmutable(path, body)
	}
	if err := writeAtomicFile(path, body, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func (store *filesystemObjectStore) Delete(key string) error {
	err := os.Remove(store.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (store *filesystemObjectStore) PutStream(_ context.Context, key string, body io.ReadSeeker, _ int64, _ string) error {
	path := store.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomicStream(path, body, 0o600)
}

func (store *filesystemObjectStore) OpenStream(_ context.Context, key string, _ int64) (io.ReadSeekCloser, error) {
	return os.Open(store.path(key))
}
