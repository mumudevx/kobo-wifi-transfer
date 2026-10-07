package main

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
)

func writeEPUB(t *testing.T, path string) {
	t.Helper()
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"content.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>T</dc:title><dc:identifier id="id">x</dc:identifier><dc:language>en</dc:language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"c.xhtml", `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>T</title></head><body><p>Hello world.</p></body></html>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(f.body))
	}
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	books, uploads, cache := t.TempDir(), t.TempDir(), t.TempDir()
	return &Server{
		lib:     &Library{roots: []string{uploads, books}},
		kepub:   &Kepubifier{dir: cache},
		uploads: uploads,
		convert: true,
	}, books
}

func get(t *testing.T, s *Server, target, userAgent string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("User-Agent", userAgent)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLibraryScan(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
	for _, name := range []string{"old.epub", "sub/new.PDF", "notes.txt", ".secret.epub", ".hidden/x.epub"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "old.epub"), past, past)

	lib := &Library{roots: []string{dir, filepath.Join(dir, "old.epub")}}
	books := lib.Books()
	if len(books) != 2 || books[0].Name != "new.PDF" || books[1].Name != "old.epub" {
		t.Fatalf("got %+v", books)
	}
	if b, ok := lib.Find(books[1].ID); !ok || b.Name != "old.epub" {
		t.Fatalf("Find: %+v %v", b, ok)
	}
	if _, ok := lib.Find("nope"); ok {
		t.Fatal("found a book that does not exist")
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{
		"book.epub":       "book.kepub.epub",
		"Book.EPUB":       "Book.kepub.epub",
		"book.kepub.epub": "book.kepub.epub",
		"paper.pdf":       "paper.pdf",
	} {
		if got := kepubName(in); got != want {
			t.Errorf("kepubName(%q) = %q, want %q", in, got, want)
		}
	}
	if got, want := asciiName("Suç ve Ceza – İkinci Işık.epub"), "Suc ve Ceza _ Ikinci Isik.epub"; got != want {
		t.Errorf("asciiName = %q, want %q", got, want)
	}
}

func TestIndex(t *testing.T) {
	s, books := newTestServer(t)
	writeEPUB(t, filepath.Join(books, "Suç ve Ceza.epub"))
	os.WriteFile(filepath.Join(books, "paper.pdf"), []byte("%PDF-1.4"), 0o644)

	mac := get(t, s, "/", "Mozilla/5.0 (Macintosh)").Body.String()
	for _, want := range []string{"Suç ve Ceza.epub", "Su%C3%A7%20ve%20Ceza.kepub.epub", "plain EPUB", "paper.pdf", `action="/upload"`} {
		if !strings.Contains(mac, want) {
			t.Errorf("index is missing %q", want)
		}
	}
	if !strings.Contains(mac, `href="/?t=`) {
		t.Error("Reload link has no cache-busting query")
	}

	// A decomposed (NFD) file name must be listed and linked in NFC.
	writeEPUB(t, filepath.Join(books, norm.NFD.String("Şöğüt.epub")))
	nfd := get(t, s, "/", "").Body.String()
	if !strings.Contains(nfd, "Şöğüt.epub") || !strings.Contains(nfd, url.PathEscape("Şöğüt.kepub.epub")) {
		t.Errorf("NFD name was not normalized:\n%s", nfd)
	}

	kobo := get(t, s, "/", "Mozilla/5.0 (Linux; Kobo Touch 0376/4.38.23171)").Body.String()
	if strings.Contains(kobo, `action="/upload"`) {
		t.Error("Kobo page shows the upload form")
	}

	s.convert, s.ascii = false, true
	plain := get(t, s, "/", "").Body.String()
	if strings.Contains(plain, "kepub") || !strings.Contains(plain, "/Suc%20ve%20Ceza.epub") {
		t.Errorf("no-kepub ascii index is wrong:\n%s", plain)
	}
}

func TestDownload(t *testing.T) {
	s, books := newTestServer(t)
	writeEPUB(t, filepath.Join(books, "book.epub"))
	os.WriteFile(filepath.Join(books, "paper.pdf"), []byte("%PDF-1.4"), 0o644)
	ids := map[string]string{}
	for _, b := range s.lib.Books() {
		ids[b.Name] = b.ID
	}

	pdf := get(t, s, "/dl/"+ids["paper.pdf"]+"/paper.pdf", "")
	if pdf.Code != 200 || pdf.Header().Get("Content-Type") != "application/pdf" || pdf.Body.String() != "%PDF-1.4" {
		t.Fatalf("pdf: %d %q", pdf.Code, pdf.Header().Get("Content-Type"))
	}

	original, _ := os.ReadFile(filepath.Join(books, "book.epub"))
	plain := get(t, s, "/dl/"+ids["book.epub"]+"/book.epub", "")
	if plain.Code != 200 || !bytes.Equal(plain.Body.Bytes(), original) {
		t.Fatalf("plain epub was not served untouched: %d", plain.Code)
	}

	for range 2 { // second pass hits the cache
		rec := get(t, s, "/dl/"+ids["book.epub"]+"/book.kepub.epub", "")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/epub+zip" {
			t.Fatalf("kepub: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
		}
		zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		f, err := zr.Open("c.xhtml")
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(f)
		if !strings.Contains(string(content), "koboSpan") {
			t.Fatalf("content was not converted to KEPUB:\n%s", content)
		}
	}

	if rec := get(t, s, "/dl/deadbeef/book.epub", ""); rec.Code != 404 {
		t.Fatalf("unknown id: %d", rec.Code)
	}
}

func TestUpload(t *testing.T) {
	s, _ := newTestServer(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, content := range map[string]string{"../../escape.epub": "book", "virus.exe": "nope"} {
		w, _ := form.CreateFormFile("file", name)
		w.Write([]byte(content))
	}
	form.Close()

	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/?skipped=1" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	entries, _ := os.ReadDir(s.uploads)
	if len(entries) != 1 || entries[0].Name() != "escape.epub" {
		t.Fatalf("uploads dir: %v", entries)
	}
	if books := s.lib.Books(); len(books) != 1 || books[0].Name != "escape.epub" {
		t.Fatalf("library: %+v", books)
	}
}
