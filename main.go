package main

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func main() {
	if filename, found := os.LookupEnv("APP_ENV_FILE"); found {
		_ = godotenv.Load(filename)
	}

	isTerm := isatty.IsTerminal(os.Stdout.Fd())
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = log.Output(
		zerolog.ConsoleWriter{
			Out:     os.Stderr,
			NoColor: !isTerm,
		},
	)

	app := &cli.App{
		Name:  "md2html",
		Usage: "Convert markdown to html",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "source",
				Aliases:  []string{"src"},
				Usage:    "Source folder containing markdown files",
				EnvVars:  []string{"APP_SOURCE_FOLDER"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "destination",
				Aliases:  []string{"dst"},
				Usage:    "Destination folder to store html files",
				EnvVars:  []string{"APP_DESTINATION_FOLDER"},
				Required: true,
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("failed to run app")
	}
}

func run(c *cli.Context) error {
	source := c.String("source")

	entries, err := os.ReadDir(source)
	if err != nil {
		log.Error().Err(err).Msg("failed to read source folder")
		return err
	}

	for _, entry := range entries {
		filename := entry.Name()
		if !isMarkdownFile(filename) {
			log.Info().Str("filename", filename).Msg("skip non-markdown file")
			continue
		}

		log.Info().Str("filename", filename).Msg("processing markdown file")
	}

	return nil
}

func isMarkdownFile(filename string) bool {
	return isMarkdownExtension(filepath.Ext(filename))
}

func isMarkdownExtension(ext string) bool {
	switch ext {
	case ".md", ".markdown":
		return true
	}
	return false
}
