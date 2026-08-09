package media

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

func ParseOggOpus(reader io.Reader, maxBytes int64) ([][]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("invalid Ogg size limit")
	}
	limited := &countingReader{reader: bufio.NewReader(io.LimitReader(reader, maxBytes+1)), max: maxBytes}
	packets := make([][]byte, 0, 512)
	current := bytes.NewBuffer(nil)
	var streamSerial uint32
	var expectedSequence uint32
	firstPage := true
	seenEOS := false
	for {
		header := make([]byte, 27)
		_, err := io.ReadFull(limited, header)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read Ogg header: %w", err)
		}
		if !bytes.Equal(header[:4], []byte("OggS")) || header[4] != 0 {
			return nil, fmt.Errorf("invalid Ogg page header")
		}
		if seenEOS {
			return nil, fmt.Errorf("data after Ogg end-of-stream page")
		}
		headerType := header[5]
		serial := binary.LittleEndian.Uint32(header[14:18])
		sequence := binary.LittleEndian.Uint32(header[18:22])
		if firstPage {
			if sequence != 0 || headerType&0x02 == 0 || headerType&0x01 != 0 {
				return nil, fmt.Errorf("invalid Ogg beginning-of-stream page")
			}
			streamSerial, expectedSequence, firstPage = serial, sequence, false
		} else if (current.Len() != 0) != (headerType&0x01 != 0) {
			return nil, fmt.Errorf("invalid Ogg packet continuation")
		}
		if serial != streamSerial || sequence != expectedSequence {
			return nil, fmt.Errorf("non-contiguous Ogg stream")
		}
		expectedSequence++
		segmentCount := int(header[26])
		lacing := make([]byte, segmentCount)
		if _, err := io.ReadFull(limited, lacing); err != nil {
			return nil, fmt.Errorf("read Ogg lacing table: %w", err)
		}
		pageSize := 0
		for _, length := range lacing {
			pageSize += int(length)
		}
		pageData := make([]byte, pageSize)
		if _, err := io.ReadFull(limited, pageData); err != nil {
			return nil, fmt.Errorf("read Ogg page data: %w", err)
		}
		offset := 0
		for _, segmentLength := range lacing {
			length := int(segmentLength)
			_, _ = current.Write(pageData[offset : offset+length])
			offset += length
			if segmentLength < 255 {
				packets = append(packets, append([]byte(nil), current.Bytes()...))
				current.Reset()
			}
		}
		seenEOS = headerType&0x04 != 0
	}
	if limited.exceeded || firstPage || !seenEOS || current.Len() != 0 || len(packets) < 3 {
		return nil, fmt.Errorf("invalid or truncated Ogg stream")
	}
	if !bytes.HasPrefix(packets[0], []byte("OpusHead")) || !bytes.HasPrefix(packets[1], []byte("OpusTags")) {
		return nil, fmt.Errorf("Ogg stream is not Opus")
	}
	frames := packets[2:]
	for _, frame := range frames {
		if len(frame) == 0 || len(frame) > 1000 {
			return nil, fmt.Errorf("invalid Opus frame length")
		}
	}
	return frames, nil
}

type countingReader struct {
	reader   io.Reader
	count    int64
	max      int64
	exceeded bool
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.count += int64(n)
	if r.count > r.max {
		r.exceeded = true
		return n, fmt.Errorf("Ogg stream exceeds size limit")
	}
	return n, err
}
