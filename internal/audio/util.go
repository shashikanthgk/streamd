package audio

import (
	"io"
	"os"
	"sync"
)

type pipeReader struct {
	r  *os.File
	mu sync.Mutex
}

func newPipe() (*pipeReader, *os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return &pipeReader{r: r}, w, nil
}

func (p *pipeReader) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r.Read(b)
}

func (p *pipeReader) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r.Close()
}

func readFull(r io.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if err == io.EOF && total > 0 {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

func bytesToInt16LE(b []byte) []int16 {
	n := len(b) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(uint16(b[i*2]) | uint16(b[i*2+1])<<8)
	}
	return out
}

func int16ToBytesLE(pcm []int16) []byte {
	out := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		out[i*2] = byte(s)
		out[i*2+1] = byte(s >> 8)
	}
	return out
}
