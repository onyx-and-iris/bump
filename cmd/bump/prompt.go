package main

import (
	"fmt"
	"io"

	"github.com/manifoldco/promptui"
	"github.com/mattn/go-tty"
	"github.com/onyx-and-iris/bump"
)

type escInterceptReader struct {
	r   io.ReadCloser
	buf []byte
}

func newEscInterceptReader(r io.ReadCloser) *escInterceptReader {
	return &escInterceptReader{r: r}
}

func (e *escInterceptReader) Read(p []byte) (int, error) {
	if len(e.buf) > 0 {
		n := copy(p, e.buf)
		e.buf = e.buf[n:]
		return n, nil
	}

	n, err := e.r.Read(p)
	if n > 0 && p[0] == 0x1b { // ESC
		if n == 1 {
			// ESC alone → Ctrl+C (interrupt)
			p[0] = 0x03
		}
		// n > 1: part of escape sequence (e.g. arrows), pass through
	}
	return n, err
}

func (e *escInterceptReader) Close() error {
	return e.r.Close()
}

func promptTarget(currentVersion, target string) (*bump.Config, error) {
	t, err := tty.Open()
	if err != nil {
		return nil, err
	}
	defer t.Close()

	candidates := []struct {
		name   string
		config *bump.Config
	}{
		{"patch", &bump.Config{PatchDelta: 1}},
		{"minor", &bump.Config{MinorDelta: 1}},
		{"major", &bump.Config{MajorDelta: 1}},
	}

	items := make([]string, len(candidates))
	for i, c := range candidates {
		newVersion, err := bump.Version(currentVersion, c.config)
		if err != nil {
			return nil, err
		}
		items[i] = fmt.Sprintf("%s (%s -> %s)", c.name, currentVersion, newVersion)
	}

	stdin := newEscInterceptReader(t.Input())
	p := promptui.Select{
		Label:        "Bump up " + target,
		HideHelp:     true,
		Items:        items,
		Stdin:        stdin,
		Stdout:       t.Output(),
		HideSelected: true,
	}

	index, _, err := p.Run()
	if err != nil {
		return nil, err
	}

	return candidates[index].config, nil
}
