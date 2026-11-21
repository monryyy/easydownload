package engine

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// ProbeResult holds the metadata discovered before downloading.
type ProbeResult struct {
	TotalSize int64
	RangeOK   bool
	ETag      string
	LastMod   string
	Name      string
}

// Probe determines file size, range support and the suggested filename.
func Probe(ctx context.Context, client *http.Client, rawURL string) (*ProbeResult, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http and https URLs are supported")
	}
	if u.Host == "" {
		return nil, errors.New("URL has no host")
	}

	result := &ProbeResult{Name: filenameFrom("", u.Path)}

	// Try HEAD first — it is cheap and usually allowed.
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(headReq)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			result.RangeOK = strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes")
			result.ETag = resp.Header.Get("ETag")
			result.LastMod = resp.Header.Get("Last-Modified")
			if name := filenameFrom(resp.Header.Get("Content-Disposition"), u.Path); name != "" {
				result.Name = name
			}
			if n, parseErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); parseErr == nil && n > 0 {
				result.TotalSize = n
			}
			resp.Body.Close()
		} else {
			code := resp.StatusCode
			resp.Body.Close()
			// These statuses mean HEAD is not usable; fall through to ranged GET.
			if code != http.StatusMethodNotAllowed && code != http.StatusNotImplemented &&
				code != http.StatusForbidden && code != http.StatusBadRequest {
				return nil, fmt.Errorf("server returned HTTP %d", code)
			}
		}
	}

	// If HEAD did not reveal a size, ask for a single byte via GET.
	if result.TotalSize <= 0 {
		getReq, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		getReq.Header.Set("Range", "bytes=0-0")
		getResp, reqErr := client.Do(getReq)
		if reqErr != nil {
			if err != nil {
				return nil, fmt.Errorf("HEAD failed: %v; ranged GET failed: %w", err, reqErr)
			}
			return nil, reqErr
		}
		defer getResp.Body.Close()

		if getResp.StatusCode != http.StatusPartialContent && getResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("server returned HTTP %d for ranged GET", getResp.StatusCode)
		}
		if getResp.StatusCode == http.StatusPartialContent {
			result.RangeOK = true
			if n, ok := totalFromContentRange(getResp.Header.Get("Content-Range")); ok {
				result.TotalSize = n
			}
		}
		if result.TotalSize <= 0 {
			if n, parseErr := strconv.ParseInt(getResp.Header.Get("Content-Length"), 10, 64); parseErr == nil && n > 0 {
				result.TotalSize = n
			}
		}
		if name := filenameFrom(getResp.Header.Get("Content-Disposition"), u.Path); name != "" {
			result.Name = name
		}
		if getResp.Header.Get("ETag") != "" {
			result.ETag = getResp.Header.Get("ETag")
		}
		if getResp.Header.Get("Last-Modified") != "" {
			result.LastMod = getResp.Header.Get("Last-Modified")
		}
	}

	if result.TotalSize <= 0 {
		return nil, errors.New("could not determine file size")
	}
	return result, nil
}

func totalFromContentRange(header string) (int64, bool) {
	// Expected form: "bytes 0-0/12345" or "bytes 0-0/*"
	parts := strings.Split(header, "/")
	if len(parts) != 2 {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	return n, err == nil && n > 0
}

func filenameFrom(contentDisposition, urlPath string) string {
	if contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if name := decodeFilenameParam(params["filename"]); name != "" {
				return sanitizeFilename(name)
			}
			if name := decodeRFC5987(params["filename*"]); name != "" {
				return sanitizeFilename(name)
			}
		}
	}
	base := path.Base(urlPath)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(base); err == nil {
		base = decoded
	}
	return sanitizeFilename(base)
}

func decodeFilenameParam(value string) string {
	if value == "" {
		return ""
	}
	if decoded, err := (&mime.WordDecoder{}).DecodeHeader(value); err == nil {
		return decoded
	}
	return value
}

func decodeRFC5987(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, "''", 2)
	if len(parts) != 2 {
		return value
	}
	decoded, err := url.PathUnescape(parts[1])
	if err != nil {
		return parts[1]
	}
	return decoded
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = filepath.Base(name)
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}
