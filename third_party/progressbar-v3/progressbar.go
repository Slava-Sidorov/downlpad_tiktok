package progressbar

import (
	"fmt"
	"io"
	"os"
	"sync"
)

type ProgressBar struct {
	mu      sync.Mutex
	total   int64
	current int64
	out     io.Writer
}

func Default(total int64) *ProgressBar {
	bar := &ProgressBar{total: total, out: os.Stderr}
	bar.render()
	return bar
}

func (b *ProgressBar) Add(num int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current += int64(num)
	if b.current > b.total {
		b.current = b.total
	}
	b.render()
	if b.current == b.total {
		_, _ = fmt.Fprintln(b.out)
	}
	return nil
}

func (b *ProgressBar) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.out.Write(p)
}

func (b *ProgressBar) render() {
	if b.total <= 0 {
		_, _ = fmt.Fprintf(b.out, "\r%d", b.current)
		return
	}
	_, _ = fmt.Fprintf(b.out, "\r%d/%d", b.current, b.total)
}
