package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/charmbracelet/log"
	"github.com/onyx-and-iris/bump"
	"github.com/urfave/cli/v3"
)

const version = "0.0.5"

type fileData struct {
	filePath     string
	pattern      string
	content      []byte
	startIndex   int
	endIndex     int
	currentValue string
}

type fileResult struct {
	file       string
	newVersion string
	err        error
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

					result, err := promptTarget(info.currentValue, info.filePath)
					if err != nil {
						return fmt.Errorf("error prompting user: %w", err)
					}

					var config *bump.Config
					switch result {
					case promptResultPatch:
						config = &bump.Config{PatchDelta: 1}
					case promptResultMinor:
						config = &bump.Config{MinorDelta: 1}
					case promptResultMajor:
						config = &bump.Config{MajorDelta: 1}
					default:
						return errors.New("invalid prompt result")
					}

					return createVersionBumpAction(config)(ctx, cmd)
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
		filePath:     file,
		pattern:      re.String(),
		content:      content,
		startIndex:   start,
		endIndex:     end,
		currentValue: string(content[start:end]),
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
					resultChan <- fileResult{file: file, err: err}
					return
				}

				if config == nil {
					fmt.Println(info.currentValue)
					resultChan <- fileResult{file: file, err: nil}
					return
				}

				newVersion, err := bump.Version(info.currentValue, config)
				if err != nil {
					resultChan <- fileResult{file: info.filePath, err: fmt.Errorf("error bumping version in file %q: %w", info.filePath, err)}
					return
				}

				if cmd.Bool("write") {
					if err := writeToFile(info, newVersion); err != nil {
						resultChan <- fileResult{file: info.filePath, err: fmt.Errorf("error writing file %q: %w", info.filePath, err)}
						return
					}
					log.Debugf("updated file %q to version %s", info.filePath, newVersion)
				}

				resultChan <- fileResult{file: info.filePath, newVersion: newVersion, err: nil}
			}(file)
		}

		if config != nil {
			return showResults(cmd, resultChan)
		}

		for range numFiles {
			result := <-resultChan
			if result.err != nil {
				return result.err
			}
		}
		return nil
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
		results[result.file] = result
	}

	var errors []error
	var successCount int

	for _, file := range cmd.StringSlice("file") {
		result := results[file]
		if result.err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", file, result.err))
		} else {
			if result.newVersion != "" {
				fmt.Println(result.newVersion)
			}
			successCount++
		}
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
