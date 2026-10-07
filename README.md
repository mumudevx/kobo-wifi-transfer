# kobo-wifi-transfer

Send EPUB and PDF files from a computer to a Kobo e-reader over Wi-Fi. No cable, no cloud account, nothing installed on the Kobo.

`kobo-send` runs a small web server on your computer. The Kobo's built-in browser opens it and downloads the books straight into the library.

## Install

With [Homebrew](https://brew.sh) (macOS or Linux):

```sh
brew tap mumudevx/tap
brew trust mumudevx/tap
brew install kobo-send
```

Homebrew 6 refuses to install from a third-party tap until it is trusted; on older versions skip the middle line.

Or with [Go](https://go.dev/dl/) 1.26 or newer. This puts the binary in `~/go/bin`, which must be on your `PATH`:

```sh
go install github.com/mumudevx/kobo-wifi-transfer/cmd/kobo-send@latest
```

## Use

```sh
kobo-send book.epub paper.pdf    # specific files
kobo-send ~/Books                # every .epub and .pdf in a folder
kobo-send                        # start empty, add books from the browser
```

It prints two addresses:

```
On the Kobo:  More > Beta Features > Web Browser > http://192.168.1.23:8765
On this Mac:  http://localhost:8765 (drop more books here)
```

On the Kobo, open the first address and tap a book. It downloads and shows up in the library. Bookmark the page so you only type the address once.

Both devices must be on the same Wi-Fi network. If macOS asks whether to allow incoming connections, allow it.

## Options

| Flag | Effect |
| --- | --- |
| `-port 8765` | Port to listen on. |
| `-no-kepub` | Serve EPUBs unchanged. |
| `-ascii` | Strip non-ASCII characters from download file names. Try this if a book with accents in its name fails to download. |

## KEPUB

EPUBs are converted to Kobo's KEPUB format on download, using [kepubify](https://github.com/pgaskin/kepubify). KEPUB gives faster page turns, reading statistics and better typography on Kobo. Each EPUB also has a "plain EPUB" link if you want the original file. PDFs are never modified.

## Notes

- Tested on a Kobo Clara HD. Any Kobo with the beta web browser should work, since it relies on the same download behaviour as [send2ereader](https://github.com/daniel-j/send2ereader).
- There is no authentication. Anyone on your network can download the listed books and upload files while the server is running. Stop it with Ctrl+C when you are done.
- Books added through the browser are kept in a temporary folder and deleted when the server stops.
- If your Mac's IP address changes, the bookmark on the Kobo stops working. Reserve a fixed address for the Mac in your router to avoid that.

## License

[MIT](LICENSE)
