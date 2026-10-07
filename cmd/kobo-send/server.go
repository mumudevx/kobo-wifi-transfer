package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed page.html
var pageHTML string

var page = template.Must(template.New("page").Parse(pageHTML))

const maxUpload = 1 << 30

type Server struct {
	lib     *Library
	kepub   *Kepubifier
	uploads string
	// convert offers EPUBs as KEPUB; ascii folds download names to ASCII.
	convert bool
	ascii   bool
}

type bookView struct {
	Name     string
	Size     string
	URL      string
	PlainURL string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /dl/{id}/{name}", s.download)
	mux.HandleFunc("POST /upload", s.upload)
	return mux
}

// The Kobo browser names the saved file after the last URL path segment, so
// the download name lives in the URL rather than in a header.
func (s *Server) link(b Book, name string) string {
	if s.ascii {
		name = asciiName(name)
	}
	return "/dl/" + b.ID + "/" + url.PathEscape(name)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	var views []bookView
	for _, b := range s.lib.Books() {
		v := bookView{Name: b.Name, Size: humanSize(b.Size), URL: s.link(b, b.Name)}
		if s.convert && isEPUB(b.Name) && !isKepub(b.Name) {
			v.URL, v.PlainURL = s.link(b, kepubName(b.Name)), v.URL
		}
		views = append(views, v)
	}
	skipped, _ := strconv.Atoi(r.URL.Query().Get("skipped"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The Kobo browser serves a revisited URL from its cache whatever these
	// say, so the Reload link also carries a fresh query string every time.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	page.Execute(w, map[string]any{
		"Reload":  fmt.Sprintf("/?t=%d", time.Now().UnixNano()),
		"Books":   views,
		"Kobo":    strings.Contains(r.UserAgent(), "Kobo"),
		"Skipped": skipped,
	})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	b, ok := s.lib.Find(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	name := r.PathValue("name")
	path := b.Path
	if isKepub(name) && !isKepub(b.Name) {
		converted, err := s.kepub.Convert(r.Context(), b)
		if err != nil {
			log.Printf("convert %s: %v", b.Name, err)
			http.Error(w, "KEPUB conversion failed: "+err.Error()+"\nGo back and use the plain EPUB link.", http.StatusInternalServerError)
			return
		}
		path = converted
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	if isEPUB(b.Name) {
		w.Header().Set("Content-Type", "application/epub+zip")
	} else {
		w.Header().Set("Content-Type", "application/pdf")
	}
	log.Printf("sending %s to %s", name, r.RemoteAddr)
	http.ServeContent(w, r, "", b.Mod, f)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	parts, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	skipped := 0
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if part.FileName() == "" {
			continue
		}
		name := filepath.Base(part.FileName())
		if !supported(name) {
			skipped++
			continue
		}
		if err := saveUpload(filepath.Join(s.uploads, name), part); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("added %s", name)
	}
	target := "/"
	if skipped > 0 {
		target = fmt.Sprintf("/?skipped=%d", skipped)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func saveUpload(dst string, src io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
