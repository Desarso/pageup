package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/desarso/pageup/internal/api"
)

const pageMetadataVersion = 1

var errPageForbidden = errors.New("page update forbidden")

type pageMetadata struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	OwnerKeyID string    `json:"owner_key_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Revision   uint64    `json:"revision"`
}

func (server *Server) createPage(id string, key api.Key, body []byte) (bool, pageMetadata, error) {
	server.pages.Lock()
	defer server.pages.Unlock()

	htmlPath, metadataPath := server.pagePaths(id)
	created, err := writeImmutable(htmlPath, body)
	if err != nil {
		return false, pageMetadata{}, err
	}
	if !created {
		metadata, err := readPageMetadata(metadataPath, id)
		if errors.Is(err, os.ErrNotExist) {
			if key.Role != RoleAdmin {
				return false, pageMetadata{}, errContentConflict
			}
			info, statErr := os.Stat(htmlPath)
			if statErr != nil {
				return false, pageMetadata{}, statErr
			}
			metadata = newPageMetadata(id, key.ID, info.ModTime())
			if err := writePageMetadata(metadataPath, metadata); err != nil {
				return false, pageMetadata{}, err
			}
			return false, metadata, nil
		}
		if err != nil {
			return false, pageMetadata{}, err
		}
		if metadata.OwnerKeyID != key.ID && key.Role != RoleAdmin {
			return false, pageMetadata{}, errContentConflict
		}
		return false, metadata, nil
	}

	metadata := newPageMetadata(id, key.ID, server.config.Now())
	if err := writePageMetadata(metadataPath, metadata); err != nil {
		if removeErr := os.Remove(htmlPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return false, pageMetadata{}, fmt.Errorf("write metadata: %w (remove incomplete page: %v)", err, removeErr)
		}
		return false, pageMetadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return true, metadata, nil
}

func (server *Server) updatePage(id string, key api.Key, body []byte) (bool, pageMetadata, error) {
	server.pages.Lock()
	defer server.pages.Unlock()

	htmlPath, metadataPath := server.pagePaths(id)
	existing, err := os.ReadFile(htmlPath)
	if err != nil {
		return false, pageMetadata{}, err
	}

	metadataMissing := false
	metadata, err := readPageMetadata(metadataPath, id)
	if errors.Is(err, os.ErrNotExist) {
		if key.Role != RoleAdmin {
			return false, pageMetadata{}, errPageForbidden
		}
		metadataMissing = true
		info, statErr := os.Stat(htmlPath)
		if statErr != nil {
			return false, pageMetadata{}, statErr
		}
		metadata = newPageMetadata(id, key.ID, info.ModTime())
	} else if err != nil {
		return false, pageMetadata{}, err
	} else if metadata.OwnerKeyID != key.ID && key.Role != RoleAdmin {
		return false, pageMetadata{}, errPageForbidden
	}

	if bytes.Equal(existing, body) {
		if metadataMissing {
			if err := writePageMetadata(metadataPath, metadata); err != nil {
				return false, pageMetadata{}, err
			}
		}
		return false, metadata, nil
	}

	updated := metadata
	updated.Revision++
	updated.UpdatedAt = server.config.Now().UTC()
	if err := writeAtomicFile(htmlPath, body, 0o600); err != nil {
		return false, pageMetadata{}, err
	}
	if err := writePageMetadata(metadataPath, updated); err != nil {
		if rollbackErr := writeAtomicFile(htmlPath, existing, 0o600); rollbackErr != nil {
			return false, pageMetadata{}, fmt.Errorf("write metadata: %w (restore page: %v)", err, rollbackErr)
		}
		return false, pageMetadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return true, updated, nil
}

func (server *Server) pagePaths(id string) (string, string) {
	base := filepath.Join(server.config.DataDir, "pages", id)
	return base + ".html", base + ".json"
}

func newPageMetadata(id, ownerKeyID string, createdAt time.Time) pageMetadata {
	createdAt = createdAt.UTC()
	return pageMetadata{
		Version:    pageMetadataVersion,
		ID:         id,
		OwnerKeyID: ownerKeyID,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		Revision:   1,
	}
}

func readPageMetadata(path, id string) (pageMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pageMetadata{}, err
	}
	var metadata pageMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return pageMetadata{}, fmt.Errorf("parse page metadata: %w", err)
	}
	if metadata.Version != pageMetadataVersion || metadata.ID != id || metadata.OwnerKeyID == "" || metadata.CreatedAt.IsZero() || metadata.UpdatedAt.IsZero() || metadata.Revision == 0 {
		return pageMetadata{}, errors.New("invalid page metadata")
	}
	return metadata, nil
}

func writePageMetadata(path string, metadata pageMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomicFile(path, data, 0o600)
}

func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".pageup-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
