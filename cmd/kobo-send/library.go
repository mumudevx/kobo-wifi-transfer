package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Book is one file offered to the Kobo.
type Book struct {
	ID   string
	Path string
	// Name is the file name in NFC. macOS often stores names decomposed
	// ("c" + combining cedilla), which the Kobo browser renders as garbage.
	Name string
	Size int64
	Mod  time.Time
}

// Library is a set of files and directories, rescanned on every listing so
// books added while the server runs show up on the next page load.
type Library struct {
	roots []string
}

func supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".epub", ".pdf":
		return !strings.HasPrefix(name, ".")
	}
	return false
}

func bookID(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:4])
}

// Books returns every supported file under the roots, newest first.
func (l *Library) Books() []Book {
	seen := map[string]bool{}
	var books []Book
	add := func(path string, d fs.DirEntry) {
		info, err := d.Info()
		if err != nil || !supported(d.Name()) {
			return
		}
		id := bookID(path)
		if seen[id] {
			return
		}
		seen[id] = true
		books = append(books, Book{ID: id, Path: path, Name: norm.NFC.String(d.Name()), Size: info.Size(), Mod: info.ModTime()})
	}
	for _, root := range l.roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			add(path, d)
			return nil
		})
	}
	sort.SliceStable(books, func(i, j int) bool {
		if !books[i].Mod.Equal(books[j].Mod) {
			return books[i].Mod.After(books[j].Mod)
		}
		return books[i].Name < books[j].Name
	})
	return books
}

func (l *Library) Find(id string) (Book, bool) {
	for _, b := range l.Books() {
		if b.ID == id {
			return b, true
		}
	}
	return Book{}, false
}

func isEPUB(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".epub")
}

func isKepub(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".kepub.epub")
}

// kepubName turns "book.epub" into "book.kepub.epub", the double extension
// Kobo needs to open a file with its KEPUB renderer.
func kepubName(name string) string {
	if isKepub(name) || !isEPUB(name) {
		return name
	}
	return name[:len(name)-len(".epub")] + ".kepub.epub"
}

var asciiPairs = strings.NewReplacer("ı", "i", "ß", "ss", "ø", "o", "Ø", "O", "æ", "ae", "Æ", "AE", "ł", "l", "Ł", "L", "đ", "d", "Đ", "D")

// asciiName strips diacritics and replaces whatever is still not ASCII.
func asciiName(name string) string {
	fold := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	if folded, _, err := transform.String(fold, asciiPairs.Replace(name)); err == nil {
		name = folded
	}
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return '_'
		}
		return r
	}, name)
}
