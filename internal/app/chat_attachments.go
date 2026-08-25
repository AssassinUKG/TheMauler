package app

import (
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mauler/internal/tools"
)

// SelectChatFiles opens a native multi-file picker and returns metadata-only
// attachments. File contents stay on disk so the agent can use the bounded read
// tool and the configured extractors instead of putting a whole large file into
// one model request.
func (a *App) SelectChatFiles() ([]ChatAttachment, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("app is not ready")
	}
	paths, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Attach files to chat",
		DefaultDirectory: a.GetWorkingDir(),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "All files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	attachments := make([]ChatAttachment, 0, len(paths))
	for _, path := range paths {
		attachment, err := prepareChatAttachmentPath(path)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

// PrepareChatAttachmentPath validates a path pasted or dropped into Chat. A
// successful result is explicit read-only authority for that exact file only;
// it is not authority to inspect sibling files or execute instructions found in
// the document.
func (a *App) PrepareChatAttachmentPath(path string) (ChatAttachment, error) {
	return prepareChatAttachmentPath(path)
}

func prepareChatAttachmentPath(raw string) (ChatAttachment, error) {
	path, err := normaliseChatAttachmentPath(raw)
	if err != nil {
		return ChatAttachment{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ChatAttachment{}, fmt.Errorf("file does not exist: %s", path)
		}
		return ChatAttachment{}, fmt.Errorf("inspect attached file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ChatAttachment{}, fmt.Errorf("chat attachments must be files, not folders: %s", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ChatAttachment{}, fmt.Errorf("resolve attached file: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(abs))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = fallbackAttachmentMIME(ext)
	}
	return ChatAttachment{
		Name: filepath.Base(abs),
		Kind: chatAttachmentKind(ext, mimeType),
		MIME: mimeType,
		Path: filepath.Clean(abs),
		Size: info.Size(),
	}, nil
}

func normaliseChatAttachmentPath(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "\"'")
	if raw == "" {
		return "", fmt.Errorf("file path is empty")
	}
	if strings.HasPrefix(strings.ToLower(raw), "file://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("invalid file URL: %w", err)
		}
		path, err := url.PathUnescape(parsed.Path)
		if err != nil {
			return "", fmt.Errorf("invalid escaped file path: %w", err)
		}
		if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
			path = `\\` + parsed.Host + filepath.FromSlash(path)
		} else if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		raw = path
	}
	return tools.NormalizeHostPath(raw), nil
}

func chatAttachmentKind(ext, mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case ext == ".pdf" || mimeType == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mimeType, "text/") || isDocumentAttachmentExtension(ext):
		return "document"
	default:
		return "file"
	}
}

func fallbackAttachmentMIME(ext string) string {
	switch ext {
	case ".json", ".jsonl":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	default:
		if isDocumentAttachmentExtension(ext) {
			return "text/plain; charset=utf-8"
		}
		return "application/octet-stream"
	}
}

func isDocumentAttachmentExtension(ext string) bool {
	switch ext {
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".jsonl", ".xml", ".yaml", ".yml", ".toml", ".ini", ".log",
		".go", ".ts", ".tsx", ".js", ".jsx", ".css", ".html", ".py", ".ps1", ".sh", ".sql":
		return true
	default:
		return false
	}
}
