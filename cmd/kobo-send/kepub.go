package main

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pgaskin/kepubify/v4/kepub"
)

// Kepubifier converts EPUBs to KEPUB on first download and caches the result.
type Kepubifier struct {
	dir string
	mu  sync.Mutex
}

// Convert returns the path of the KEPUB version of b.
func (k *Kepubifier) Convert(ctx context.Context, b Book) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	out := filepath.Join(k.dir, fmt.Sprintf("%s-%d-%d.kepub.epub", b.ID, b.Mod.UnixNano(), b.Size))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}

	src, err := zip.OpenReader(b.Path)
	if err != nil {
		return "", err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(k.dir, "converting-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if err := kepub.NewConverter().Convert(ctx, tmp, src); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return out, os.Rename(tmp.Name(), out)
}
