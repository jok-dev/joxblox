package renderdoc

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"
)

// BufferStore opens the companion .zip archive produced by
// `renderdoccmd convert -c zip.xml` and reads buffers by their XML-referenced
// ID. Buffers are named as 6-digit zero-padded decimal filenames in the ZIP.
type BufferStore struct {
	reader  *zip.ReadCloser
	filesByID map[string]*zip.File
}

// OpenBufferStore opens the ZIP sibling of the given xmlPath. It accepts
// either the .zip.xml path or the .zip path directly and normalises.
func OpenBufferStore(path string) (*BufferStore, error) {
	zipPath := strings.TrimSuffix(path, ".xml")
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open capture zip %q: %w", zipPath, err)
	}
	store := &BufferStore{
		reader:    reader,
		filesByID: make(map[string]*zip.File, len(reader.File)),
	}
	for _, file := range reader.File {
		// Entries are named "000000", "000001", ... — strip leading zeros to
		// get the decimal ID referenced by <buffer>ID</buffer> in the XML.
		id := strings.TrimLeft(file.Name, "0")
		if id == "" {
			id = "0"
		}
		store.filesByID[id] = file
	}
	return store, nil
}

// Close releases the underlying ZIP file handle.
func (store *BufferStore) Close() error {
	if store == nil || store.reader == nil {
		return nil
	}
	return store.reader.Close()
}

// ReadBuffer returns the raw bytes of the buffer with the given XML-referenced
// ID (the text inside a <buffer>…</buffer> element).
func (store *BufferStore) ReadBuffer(id string) ([]byte, error) {
	file, ok := store.filesByID[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("buffer %q not found in capture zip", id)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open buffer %q: %w", id, err)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
