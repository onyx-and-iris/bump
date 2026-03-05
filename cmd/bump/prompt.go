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

type promptResult int

const (
	promptResultNone promptResult = iota
	promptResultPatch
	promptResultMinor
	promptResultMajor
)

func promptTarget(currentVersion, target string) (promptResult, error) {
	t, err := tty.Open()
	if err != nil {
		return promptResultNone, err
	}
	defer t.Close()

	candidates := []struct {
		name   string
		delta  [3]uint64 // major, minor, patch
		result promptResult
	}{
		{"patch", [3]uint64{0, 0, 1}, promptResultPatch},
		{"minor", [3]uint64{0, 1, 0}, promptResultMinor},
		{"major", [3]uint64{1, 0, 0}, promptResultMajor},
	}

	items := make([]string, len(candidates))
	for i, c := range candidates {
		config := &bump.Config{
			MajorDelta: c.delta[0],
			MinorDelta: c.delta[1],
			PatchDelta: c.delta[2],
		}
		newVersion, err := bump.Version(currentVersion, config)
		if err != nil {
			return promptResultNone, err
		}
		items[i] = fmt.Sprintf("%s (%s -> %s)", c.name, currentVersion, newVersion)
	}

	stdin := newEscInterceptReader(t.Input())
	p := promptui.Select{
		Label:    "Bump up " + target,
		HideHelp: true,
		Items:    items,
		Stdin:    stdin,
		Stdout:   t.Output(),
	}

	index, _, err := p.Run()
	if err != nil {
		return promptResultNone, err
	}

	return candidates[index].result, nil
}
