package compose

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

// Attachment holds the metadata for a file the user wants to send.
// The file is NOT copied — Path is the original location on disk, read
// only at EML-build time inside BuildEML.
type Attachment struct {
	Path     string
	Filename string
	MIMEType string
	Size     int64
}

// NewAttachment reads metadata for the file at path and returns an Attachment.
// Returns an error if the path does not exist, is not a regular file, or
// cannot be read for MIME sniffing.
func NewAttachment(path string) (Attachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, err
	}
	if !info.Mode().IsRegular() {
		return Attachment{}, fmt.Errorf("%s is not a file", filepath.Base(path))
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = sniffMIME(path)
	}

	return Attachment{
		Path:     filepath.Clean(path),
		Filename: filepath.Base(path),
		MIMEType: mimeType,
		Size:     info.Size(),
	}, nil
}

// sniffMIME reads the first 512 bytes and uses http.DetectContentType.
// Falls back to application/octet-stream on any error.
func sniffMIME(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}
