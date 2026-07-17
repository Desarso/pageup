package sitebundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MediaType       = "application/vnd.pageup.site+zip"
	MaxFiles        = 100
	DefaultMaxBytes = int64(5 << 20)
)

var zipEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

type Site struct {
	Files map[string][]byte
}

func Pack(root string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("maximum site size must be positive")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("site path is not a directory")
	}

	type inputFile struct {
		absolute string
		relative string
		size     int64
	}
	var files []inputFile
	var total int64
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == root {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("site contains a symbolic link: %s", name)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("site contains a non-regular file: %s", name)
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !strings.EqualFold(path.Ext(relative), ".html") {
			return fmt.Errorf("site directories may contain only .html files: %s", relative)
		}
		if len(files) >= MaxFiles {
			return fmt.Errorf("site exceeds the %d HTML file limit", MaxFiles)
		}
		if info.Size() > maxBytes-total {
			return fmt.Errorf("site HTML exceeds the %s upload limit", formatBytes(maxBytes))
		}
		total += info.Size()
		files = append(files, inputFile{absolute: name, relative: relative, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("site directory contains no HTML files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relative < files[j].relative })
	index := sort.Search(len(files), func(i int) bool { return files[i].relative >= "index.html" })
	if index == len(files) || files[index].relative != "index.html" {
		return nil, errors.New("site directory must contain index.html at its root")
	}

	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, input := range files {
		data, err := os.ReadFile(input.absolute)
		if err != nil {
			archive.Close()
			return nil, err
		}
		if int64(len(data)) != input.size {
			archive.Close()
			return nil, fmt.Errorf("site file changed while it was being packed: %s", input.relative)
		}
		header := &zip.FileHeader{Name: input.relative, Method: zip.Deflate}
		header.SetModTime(zipEpoch)
		header.SetMode(0o600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			archive.Close()
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			archive.Close()
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func Parse(data []byte, maxBytes int64) (Site, error) {
	if maxBytes <= 0 {
		return Site{}, errors.New("maximum site size must be positive")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Site{}, errors.New("site is not a valid ZIP archive")
	}
	if len(archive.File) == 0 {
		return Site{}, errors.New("site archive contains no HTML files")
	}
	if len(archive.File) > MaxFiles {
		return Site{}, fmt.Errorf("site exceeds the %d HTML file limit", MaxFiles)
	}

	site := Site{Files: make(map[string][]byte, len(archive.File))}
	var total uint64
	for _, file := range archive.File {
		name := file.Name
		if !validName(name) {
			return Site{}, fmt.Errorf("site contains an invalid path: %s", name)
		}
		if !strings.EqualFold(path.Ext(name), ".html") {
			return Site{}, fmt.Errorf("site directories may contain only .html files: %s", name)
		}
		if !file.Mode().IsRegular() {
			return Site{}, fmt.Errorf("site contains a non-regular file: %s", name)
		}
		if _, exists := site.Files[name]; exists {
			return Site{}, fmt.Errorf("site contains a duplicate path: %s", name)
		}
		if file.UncompressedSize64 > uint64(maxBytes)-total {
			return Site{}, fmt.Errorf("site HTML exceeds the %s upload limit", formatBytes(maxBytes))
		}
		total += file.UncompressedSize64
		reader, err := file.Open()
		if err != nil {
			return Site{}, fmt.Errorf("open site file %s: %w", name, err)
		}
		contents, readErr := io.ReadAll(io.LimitReader(reader, int64(file.UncompressedSize64)+1))
		closeErr := reader.Close()
		if readErr != nil {
			return Site{}, fmt.Errorf("read site file %s: %w", name, readErr)
		}
		if closeErr != nil {
			return Site{}, fmt.Errorf("close site file %s: %w", name, closeErr)
		}
		if uint64(len(contents)) != file.UncompressedSize64 {
			return Site{}, fmt.Errorf("site file has an invalid size: %s", name)
		}
		site.Files[name] = contents
	}
	if _, exists := site.Files["index.html"]; !exists {
		return Site{}, errors.New("site directory must contain index.html at its root")
	}
	return site, nil
}

func validName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, "/") &&
		!strings.Contains(name, "\\") &&
		!strings.ContainsRune(name, '\x00') &&
		path.Clean(name) == name &&
		name != "." &&
		!strings.HasPrefix(name, "../")
}

func formatBytes(size int64) string {
	if size%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", size/(1<<20))
	}
	return fmt.Sprintf("%d bytes", size)
}
