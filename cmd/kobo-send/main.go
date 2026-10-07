// Command kobo-send serves ebooks on the local network so a Kobo e-reader can
// download them with its built-in web browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// version is set at release time with -ldflags.
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	port := flag.Int("port", 8080, "port to listen on")
	noKepub := flag.Bool("no-kepub", false, "serve EPUBs as they are instead of converting to KEPUB")
	ascii := flag.Bool("ascii", false, "strip non-ASCII characters from download file names")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: kobo-send [flags] [file or directory ...]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("kobo-send", version)
		return
	}
	if err := run(*port, !*noKepub, *ascii, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "kobo-send:", err)
		os.Exit(1)
	}
}

func run(port int, convert, ascii bool, args []string) error {
	work, err := os.MkdirTemp("", "kobo-send-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	uploads, cache := filepath.Join(work, "uploads"), filepath.Join(work, "kepub")
	for _, dir := range []string{uploads, cache} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	}

	roots := []string{uploads}
	for _, arg := range args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return err
		}
		roots = append(roots, abs)
	}
	lib := &Library{roots: roots}
	srv := &Server{lib: lib, kepub: &Kepubifier{dir: cache}, uploads: uploads, convert: convert, ascii: ascii}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	fmt.Printf("Serving %d book(s).\n\n", len(lib.Books()))
	fmt.Printf("  On the Kobo:  More > Beta Features > Web Browser > http://%s:%d\n", lanIP(), port)
	fmt.Printf("  On this Mac:  http://localhost:%d (drop more books here)\n\n", port)
	fmt.Println("Ctrl+C to stop.")

	log.SetFlags(log.Ltime)
	httpSrv := &http.Server{Handler: srv.Handler()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()
	if err := httpSrv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// lanIP returns the address other devices on the network can reach us at.
func lanIP() string {
	// Connecting a UDP socket sends nothing; it only asks the kernel which
	// local address it would route from.
	if conn, err := net.Dial("udp", "192.0.2.1:9"); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && !addr.IP.IsLoopback() {
			return addr.IP.String()
		}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.IsPrivate() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "<this-mac-ip>"
}
