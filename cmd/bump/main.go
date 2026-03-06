package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/log"
	"github.com/onyx-and-iris/bump"
	"github.com/urfave/cli/v3"
)

var version string // Version holds the application version, set at build time using ldflags.

// versionFromBuild retrieves the version information from the build metadata.
func versionFromBuild() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unable to read version)"
	}
	return strings.Split(info.Main.Version, "-")[0]
}

type fileData struct {
	filePath       string
	pattern        string
	content        []byte
	startIndex     int
	endIndex       int
	currentVersion string
}

type fileResult struct {
	filePath       string
	pattern        string
	currentVersion string
	newVersion     string
	err            error
}

func main() {
	cli.VersionPrinter = func(cmd *cli.Command) {
		fmt.Printf("bump version: %s\n", cmd.Root().Version)
	}

	cmd := &cli.Command{
		Name:    "bump",
		Usage:   "bump version in files with regex patterns",
		Version: versionFromBuild(),
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name: "file", Aliases: []string{"f"},
				Usage:    "target file",
				Sources:  cli.EnvVars("BUMP_CLI_FILE"),
				Required: true,
			},
			&cli.StringFlag{
				Name: "pattern", Aliases: []string{"p"},
				Usage:    "regexp pattern with a capture group for the version",
				Sources:  cli.EnvVars("BUMP_CLI_PATTERN"),
				Required: true,
				Action: func(ctx context.Context, cmd *cli.Command, value string) error {
					re, err := regexp.Compile(value)
					if err != nil {
						return fmt.Errorf("invalid pattern: %w", err)
					}
					if re.NumSubexp() < 1 {
						return errors.New("pattern must contain at least one capture group for the version")
					}
					return nil
				},
			},
			&cli.BoolFlag{
				Name: "write", Aliases: []string{"w"},
				Usage:   "write result to file instead of stdout",
				Sources: cli.EnvVars("BUMP_CLI_WRITE"),
			},
			&cli.BoolFlag{
				Name: "print-pattern", Aliases: []string{"pp"},
				Usage:   "include the regex pattern in the output table",
				Sources: cli.EnvVars("BUMP_CLI_PRINT_PATTERN"),
			},
			&cli.StringFlag{
				Name: "loglevel", Aliases: []string{"l"},
				Usage:   "Set the logging level (debug, info, warn, error, fatal, panic).",
				Sources: cli.EnvVars("BUMP_CLI_LOGLEVEL"),
				Value:   "info",
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
		Commands: []*cli.Command{
			{
				Name:   "show",
				Usage:  "show current version without bumping",
				Action: createVersionBumpAction(nil),
			},
			{
				Name:   "major",
				Usage:  "bump major version",
				Action: createVersionBumpAction(&bump.Config{MajorDelta: 1}),
			},
			{
				Name:   "minor",
				Usage:  "bump minor version",
				Action: createVersionBumpAction(&bump.Config{MinorDelta: 1}),
			},
			{
				Name:   "patch",
				Usage:  "bump patch version",
				Action: createVersionBumpAction(&bump.Config{PatchDelta: 1}),
			},
			{
				Name:  "exact",
				Usage: "set exact version (e.g. 1.2.3)",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "version",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					bumpActionFn := createVersionBumpAction(&bump.Config{Exact: cmd.StringArg("version")})
					return bumpActionFn(ctx, cmd)
				},
			},
			{
				Name: "prompt", Aliases: []string{"up"},
				Usage: "interactively prompt for version bump (only supports single file)",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name: "yes", Aliases: []string{"y"},
						Usage: "skip prompt and use patch (for non-interactive environments)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if len(cmd.StringSlice("file")) > 1 {
						return errors.New(
							"prompt command does not support multiple files (use major minor or patch for multiple files)",
						)
					}

					if cmd.Bool("yes") {
						config := &bump.Config{PatchDelta: 1}
						return createVersionBumpAction(config)(ctx, cmd)
					}

					info, err := currentVersionFromFile(
						cmd.StringSlice("file")[0],
						regexp.MustCompile(cmd.String("pattern")),
					)
					if err != nil {
						return fmt.Errorf("error getting current version: %w", err)
					}

					result, err := promptTarget(info.currentVersion, info.filePath)
					if err != nil {
						return fmt.Errorf("error prompting user: %w", err)
					}
					bumpActionFn := createVersionBumpAction(result)
					return bumpActionFn(ctx, cmd)
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func currentVersionFromFile(file string, re *regexp.Regexp) (fileData, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return fileData{}, fmt.Errorf("error reading file %q: %w", file, err)
	}

	loc := re.FindSubmatchIndex(content)
	if loc == nil {
		return fileData{}, fmt.Errorf("pattern did not match in file %q", file)
	}
	if len(loc) < 4 {
		return fileData{}, fmt.Errorf("pattern must have at least 1 capture group in file %q", file)
	}
	start, end := loc[2], loc[3]

	return fileData{
		filePath:       file,
		pattern:        re.String(),
		content:        content,
		startIndex:     start,
		endIndex:       end,
		currentVersion: string(content[start:end]),
	}, nil
}

func createVersionBumpAction(config *bump.Config) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		numFiles := len(cmd.StringSlice("file"))
		resultChan := make(chan fileResult, numFiles)
		re, _ := regexp.Compile(cmd.String("pattern"))

		for _, file := range cmd.StringSlice("file") {
			go func(file string) {
				info, err := currentVersionFromFile(file, re)
				if err != nil {
					resultChan <- fileResult{filePath: file, err: err}
					return
				}

				if config == nil {
					resultChan <- fileResult{filePath: file, pattern: info.pattern, currentVersion: info.currentVersion, err: nil}
					return
				}

				newVersion, err := bump.Version(info.currentVersion, config)
				if err != nil {
					resultChan <- fileResult{filePath: info.filePath, err: fmt.Errorf("error bumping version in file %q: %w", info.filePath, err)}
					return
				}

				if cmd.Bool("write") {
					if err := writeToFile(info, newVersion); err != nil {
						resultChan <- fileResult{filePath: info.filePath, err: fmt.Errorf("error writing file %q: %w", info.filePath, err)}
						return
					}
					log.Debugf("updated file %q to version %s", info.filePath, newVersion)
				}

				resultChan <- fileResult{filePath: info.filePath, pattern: info.pattern, currentVersion: info.currentVersion, newVersion: newVersion, err: nil}
			}(file)
		}

		return showResults(cmd, resultChan)
	}
}

func writeToFile(data fileData, newVersion string) error {
	newContent := string(data.content[:data.startIndex]) + newVersion + string(data.content[data.endIndex:])
	if err := os.WriteFile(data.filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("error writing file %q: %w", data.filePath, err)
	}

	return nil
}

func showResults(cmd *cli.Command, resultChan <-chan fileResult) error {
	results := make(map[string]fileResult)
	for range cmd.StringSlice("file") {
		result := <-resultChan
		results[result.filePath] = result
	}

	var errors []error
	var successCount int
	var headers []string
	if cmd.Bool("print-pattern") {
		headers = []string{"File", "Pattern", "Current Version", "New Version"}
	} else {
		headers = []string{"File", "Current Version", "New Version"}
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#C7D2FE"))).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)

			if row == table.HeaderRow {
				return style.Bold(true).Foreground(lipgloss.Color("#5B73E8"))
			}
			isEvenRow := row%2 == 0

			switch col {
			case 0:
				if isEvenRow {
					style = style.Foreground(lipgloss.Color("#6B7FD7"))
				} else {
					style = style.Foreground(lipgloss.Color("#8A9AE3"))
				}
			case 1:
				if len(headers) == 4 {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#9CA3F0")).Italic(true)
					} else {
						style = style.Foreground(lipgloss.Color("#B5C2F5")).Italic(true)
					}
				} else {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#5B73E8"))
					} else {
						style = style.Foreground(lipgloss.Color("#7A8DED"))
					}
					style = style.Align(lipgloss.Center)
				}
			case 2:
				if len(headers) == 4 {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#5B73E8"))
					} else {
						style = style.Foreground(lipgloss.Color("#7A8DED"))
					}
				} else {
					if isEvenRow {
						style = style.Foreground(lipgloss.Color("#4F68E0"))
					} else {
						style = style.Foreground(lipgloss.Color("#6A7FE6"))
					}
				}
				style = style.Align(lipgloss.Center)
			case 3:
				if isEvenRow {
					style = style.Foreground(lipgloss.Color("#4F68E0"))
				} else {
					style = style.Foreground(lipgloss.Color("#6A7FE6"))
				}
				style = style.Align(lipgloss.Center)
			}

			return style
		})

	for _, file := range cmd.StringSlice("file") {
		result := results[file]
		if result.err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", file, result.err))
		} else {
			if result.newVersion != "" {
				if cmd.Bool("print-pattern") {
					t.Row(result.filePath, result.pattern, result.currentVersion, result.newVersion)
				} else {
					t.Row(result.filePath, result.currentVersion, result.newVersion)
				}
			} else {
				if cmd.Bool("print-pattern") {
					t.Row(result.filePath, result.pattern, result.currentVersion, "—")
				} else {
					t.Row(result.filePath, result.currentVersion, "—")
				}
			}
			successCount++
		}
	}
	if successCount > 0 {
		fmt.Println(t.String())
	}

	if len(errors) > 0 && successCount > 0 {
		log.Warnf("Completed with mixed results: %d succeeded, %d failed", successCount, len(errors))
		for _, err := range errors {
			log.Errorf("  ✗ %v", err)
		}
		return fmt.Errorf("%d files failed to process", len(errors))
	} else if len(errors) > 0 {
		log.Errorf("All %d files failed to process", len(errors))
		for _, err := range errors {
			log.Errorf("  ✗ %v", err)
		}
		if len(errors) == 1 {
			return errors[0]
		}
		return fmt.Errorf("failed to process %d files", len(errors))
	} else {
		log.Infof("Successfully processed all %d files", successCount)
		return nil
	}
}
