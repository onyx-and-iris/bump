package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/manifoldco/promptui"
	"github.com/mattn/go-tty"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v3"
)

const version = "0.0.5"

type processArgs struct {
	file       string
	majorDelta uint64
	minorDelta uint64
	patchDelta uint64
	exact      string
	showOnly   bool
	prompt     bool
	re         *regexp.Regexp
}

func (pa processArgs) SafeString() string {
	return fmt.Sprintf("processArgs{majorDelta:%d, minorDelta:%d, patchDelta:%d, exact:%q, showOnly:%t, prompt:%t}",
		pa.majorDelta, pa.minorDelta, pa.patchDelta, pa.exact, pa.showOnly, pa.prompt)
}

func main() {
	cli.VersionPrinter = func(cmd *cli.Command) {
		fmt.Printf("bump version: %s\n", cmd.Root().Version)
	}

	cmd := &cli.Command{
		Name:    "bump",
		Usage:   "bump version in files with regex patterns",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name: "file", Aliases: []string{"f"},
				Usage:    "target file",
				Required: true,
			},
			&cli.StringFlag{
				Name: "pattern", Aliases: []string{"p"},
				Usage:    "regexp pattern with a capture group for the version",
				Required: true,
			},
			&cli.BoolFlag{
				Name: "write", Aliases: []string{"w"},
				Usage: "write result to file instead of stdout",
			},
			&cli.BoolFlag{
				Name: "yes", Aliases: []string{"y"},
				Usage: "skip prompt and use patch (for non-interactive environments)",
			},
			&cli.StringFlag{
				Name: "loglevel", Aliases: []string{"l"},
				Usage: "Set the logging level (debug, info, warn, error, fatal, panic).",
				Value: "info",
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			level, err := log.ParseLevel(cmd.String("loglevel"))
			if err != nil {
				return nil, fmt.Errorf("invalid log level: %w", err)
			}
			log.SetLevel(level)
			return ctx, nil
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			var pargs processArgs

			switch cmd.Args().First() {
			case "major":
				pargs.majorDelta = 1
			case "minor":
				pargs.minorDelta = 1
			case "patch":
				pargs.patchDelta = 1
			case "up":
				pargs.prompt = true
			case "show":
				pargs.showOnly = true
			case "set":
				if cmd.Args().Len() < 2 {
					return errors.New("please specify a version to set")
				}
				pargs.exact = cmd.Args().Get(1)
			default:
				return fmt.Errorf("unknown subcommand %q", cmd.Args().First())
			}
			log.Debugf("Configuration: %s", pargs.SafeString())

			re, err := regexp.Compile(cmd.String("pattern"))
			if err != nil {
				return fmt.Errorf("invalid pattern: %w", err)
			}
			if re.NumSubexp() < 1 {
				return errors.New("pattern must contain at least one capture group for the version")
			}
			pargs.re = re

			for _, file := range cmd.StringSlice("file") {
				pargs.file = file
				if err := processFile(pargs, cmd); err != nil {
					return fmt.Errorf("processing file %q failed: %w", file, err)
				}
			}

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func processFile(pargs processArgs, cmd *cli.Command) error {
	log.Debugf("Processing file: %s", pargs.file)
	content, err := os.ReadFile(pargs.file)
	if err != nil {
		return err
	}

	loc := pargs.re.FindSubmatchIndex(content)
	if loc == nil {
		return errors.New("pattern did not match")
	}

	// loc[2], loc[3] are the start and end of the first capture group
	currentVersion := string(content[loc[2]:loc[3]])

	if pargs.showOnly {
		fmt.Println(currentVersion)
		return nil
	}

	if pargs.prompt {
		result, err := promptTarget(currentVersion, pargs.file)
		if err != nil {
			if !cmd.Bool("yes") {
				return err
			}
			pargs.patchDelta = 1
		} else {
			switch result {
			case promptResultPatch:
				pargs.patchDelta = 1
			case promptResultMinor:
				pargs.minorDelta = 1
			case promptResultMajor:
				pargs.majorDelta = 1
			}
		}
	}

	newVersion, err := bumpVersion(currentVersion, pargs)
	if err != nil {
		return fmt.Errorf("version bump failed: %w", err)
	}

	result := make([]byte, 0, len(content)+len(newVersion)-len(currentVersion))
	result = append(result, content[:loc[2]]...)
	result = append(result, []byte(newVersion)...)
	result = append(result, content[loc[3]:]...)

	if cmd.Bool("write") {
		if err := os.WriteFile(pargs.file, result, 0644); err != nil {
			return err
		}
		fmt.Println(newVersion)
		log.Debugf("File %q updated successfully", pargs.file)
	} else {
		fmt.Println(string(result))
	}
	return nil
}

func bumpVersion(version string, pargs processArgs) (string, error) {
	if pargs.exact != "" {
		ev, err := semver.StrictNewVersion(pargs.exact)
		if err != nil {
			return "", fmt.Errorf("invalid version %q: %w", pargs.exact, err)
		}
		if v, err := semver.StrictNewVersion(version); err == nil {
			if !ev.GreaterThan(v) {
				return "", fmt.Errorf("version %s is not greater than the current version %s", ev, v)
			}
		}
		return ev.String(), nil
	}

	v, err := semver.StrictNewVersion(version)
	if err != nil {
		return "", fmt.Errorf("invalid current version %q: %w", version, err)
	}

	if pargs.majorDelta > 0 {
		for i := uint64(0); i < pargs.majorDelta; i++ {
			*v = v.IncMajor()
		}
	} else if pargs.minorDelta > 0 {
		for i := uint64(0); i < pargs.minorDelta; i++ {
			*v = v.IncMinor()
		}
	} else if pargs.patchDelta > 0 {
		for i := uint64(0); i < pargs.patchDelta; i++ {
			*v = v.IncPatch()
		}
	}

	return v.String(), nil
}

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
		newVersion, err := bumpVersion(currentVersion, processArgs{
			majorDelta: c.delta[0],
			minorDelta: c.delta[1],
			patchDelta: c.delta[2],
			exact:      "",
		})
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
