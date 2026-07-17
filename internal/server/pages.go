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

type pageKind string

const (
	pageKindHTML pageKind = "html"
	pageKindSite pageKind = "site"
)

type pageMetadata struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	OwnerKeyID string    `json:"owner_key_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Revision   uint64    `json:"revision"`
}

func (server *Server) createPage(id string, key api.Key, kind pageKind, body []byte) (bool, pageMetadata, error) {
	server.pages.Lock()
	defer server.pages.Unlock()

	metadataPath := server.pageMetadataPath(id)
	existingKind, _, existing, err := server.readPageContent(id)
	if err == nil {
		if existingKind != kind || !bytes.Equal(existing, body) {
			return false, pageMetadata{}, errContentConflict
		}
		metadata, err := readPageMetadata(metadataPath, id)
		if errors.Is(err, os.ErrNotExist) {
			if key.Role != RoleAdmin {
				return false, pageMetadata{}, errContentConflict
			}
			info, statErr := os.Stat(server.pageContentPath(id, kind))
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
	if !errors.Is(err, os.ErrNotExist) {
		return false, pageMetadata{}, err
	}

	contentPath := server.pageContentPath(id, kind)
	created, err := writeImmutable(contentPath, body)
	if err != nil {
		return false, pageMetadata{}, err
	}
	if !created {
		return false, pageMetadata{}, errContentConflict
	}

	metadata := newPageMetadata(id, key.ID, server.config.Now())
	if err := writePageMetadata(metadataPath, metadata); err != nil {
		if removeErr := os.Remove(contentPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return false, pageMetadata{}, fmt.Errorf("write metadata: %w (remove incomplete page: %v)", err, removeErr)
		}
		return false, pageMetadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return true, metadata, nil
}

func (server *Server) updatePage(id string, key api.Key, kind pageKind, body []byte) (bool, pageMetadata, error) {
	server.pages.Lock()
	defer server.pages.Unlock()

	existingKind, existingPath, existing, err := server.readPageContent(id)
	if err != nil {
		return false, pageMetadata{}, err
	}
	metadataPath := server.pageMetadataPath(id)

	metadataMissing := false
	metadata, err := readPageMetadata(metadataPath, id)
	if errors.Is(err, os.ErrNotExist) {
		if key.Role != RoleAdmin {
			return false, pageMetadata{}, errPageForbidden
		}
		metadataMissing = true
		info, statErr := os.Stat(existingPath)
		if statErr != nil {
			return false, pageMetadata{}, statErr
		}
		metadata = newPageMetadata(id, key.ID, info.ModTime())
	} else if err != nil {
		return false, pageMetadata{}, err
	} else if metadata.OwnerKeyID != key.ID && key.Role != RoleAdmin {
		return false, pageMetadata{}, errPageForbidden
	}

	if existingKind == kind && bytes.Equal(existing, body) {
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
	targetPath := server.pageContentPath(id, kind)
	if existingKind != kind {
		if _, err := os.Stat(targetPath); err == nil {
			return false, pageMetadata{}, errors.New("page has conflicting stored content")
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, pageMetadata{}, err
		}
	}
	if err := writeAtomicFile(targetPath, body, 0o600); err != nil {
		return false, pageMetadata{}, err
	}
	if existingKind != kind {
		if err := os.Remove(existingPath); err != nil {
			os.Remove(targetPath)
			return false, pageMetadata{}, err
		}
	}
	if err := writePageMetadata(metadataPath, updated); err != nil {
		rollbackErr := writeAtomicFile(existingPath, existing, 0o600)
		if existingKind != kind {
			if removeErr := os.Remove(targetPath); rollbackErr == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				rollbackErr = removeErr
			}
		}
		if rollbackErr != nil {
			return false, pageMetadata{}, fmt.Errorf("write metadata: %w (restore page: %v)", err, rollbackErr)
		}
		return false, pageMetadata{}, fmt.Errorf("write metadata: %w", err)
	}
	return true, updated, nil
}

func (server *Server) pageContentPath(id string, kind pageKind) string {
	extension := ".html"
	if kind == pageKindSite {
		extension = ".site.zip"
	}
	return filepath.Join(server.config.DataDir, "pages", id+extension)
}

func (server *Server) pageMetadataPath(id string) string {
	return filepath.Join(server.config.DataDir, "pages", id+".json")
}

func (server *Server) readPageContent(id string) (pageKind, string, []byte, error) {
	htmlPath := server.pageContentPath(id, pageKindHTML)
	sitePath := server.pageContentPath(id, pageKindSite)
	html, htmlErr := os.ReadFile(htmlPath)
	site, siteErr := os.ReadFile(sitePath)
	if htmlErr == nil && siteErr == nil {
		return "", "", nil, errors.New("page has conflicting stored content")
	}
	if htmlErr == nil {
		return pageKindHTML, htmlPath, html, nil
	}
	if siteErr == nil {
		return pageKindSite, sitePath, site, nil
	}
	if !errors.Is(htmlErr, os.ErrNotExist) {
		return "", "", nil, htmlErr
	}
	if !errors.Is(siteErr, os.ErrNotExist) {
		return "", "", nil, siteErr
	}
	return "", "", nil, os.ErrNotExist
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
