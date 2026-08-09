package media

import (
	"bytes"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

var allowedAudioMIMEs = map[string]map[string]struct{}{
	".mp3":  {"audio/mpeg": {}},
	".wav":  {"audio/wav": {}, "audio/x-wav": {}, "audio/wave": {}},
	".ogg":  {"audio/ogg": {}, "application/ogg": {}},
	".opus": {"audio/ogg": {}, "audio/opus": {}, "application/ogg": {}},
	".flac": {"audio/flac": {}, "audio/x-flac": {}},
}

func ValidateUploadHeader(filename, declaredMIME string, prefix []byte) (string, string, error) {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	allowed, ok := allowedAudioMIMEs[extension]
	if !ok {
		return "", "", fmt.Errorf("unsupported audio extension")
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(declaredMIME))
	if err != nil {
		return "", "", fmt.Errorf("invalid audio MIME type")
	}
	mediaType = strings.ToLower(mediaType)
	if _, ok := allowed[mediaType]; !ok {
		return "", "", fmt.Errorf("audio MIME type does not match extension")
	}
	if !matchesAudioSignature(extension, prefix) {
		return "", "", fmt.Errorf("audio signature does not match extension")
	}
	return extension, mediaType, nil
}

func matchesAudioSignature(extension string, prefix []byte) bool {
	switch extension {
	case ".wav":
		return len(prefix) >= 12 && bytes.Equal(prefix[:4], []byte("RIFF")) && bytes.Equal(prefix[8:12], []byte("WAVE"))
	case ".ogg", ".opus":
		return len(prefix) >= 4 && bytes.Equal(prefix[:4], []byte("OggS"))
	case ".flac":
		return len(prefix) >= 4 && bytes.Equal(prefix[:4], []byte("fLaC"))
	case ".mp3":
		return len(prefix) >= 3 && (bytes.Equal(prefix[:3], []byte("ID3")) || (prefix[0] == 0xff && prefix[1]&0xe0 == 0xe0))
	default:
		return false
	}
}
