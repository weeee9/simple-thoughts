package main

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/russross/blackfriday/v2"
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
				Name:        "source",
				Aliases:     []string{"src"},
				Usage:       "Source folder containing markdown files",
				EnvVars:     []string{"APP_SOURCE_FOLDER"},
				DefaultText: "posts",
				Value:       "posts",
			},
			&cli.StringFlag{
				Name:        "destination",
				Aliases:     []string{"dst"},
				Usage:       "Destination folder to store html files",
				EnvVars:     []string{"APP_DESTINATION_FOLDER"},
				DefaultText: "html",
				Value:       "html",
			},
			&cli.StringSliceFlag{
				Name:    "templates",
				Usage:   "List of templates to use to generate html files",
				EnvVars: []string{"APP_HTML_TEMPLATES"},
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("failed to run app")
	}
}

func run(c *cli.Context) error {
	pathToSource := c.String("source")
	pathToDestination := c.String("destination")
	pathToTemplates := c.StringSlice("templates")

	entries, err := os.ReadDir(pathToSource)
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
		if err := convert(filename, pathToSource, pathToDestination, pathToTemplates...); err != nil {
			log.Error().Err(err).Str("filename", filename).Msg("failed to convert markdown file")
			return err
		}
		log.Info().Str("filename", filename).Msg("markdown file converted")
	}

	return nil
}

func convert(filename, src, dst string, templates ...string) error {
	realPath := filepath.Join(src, filename)
	md, err := os.ReadFile(realPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to read markdown file")
		return err
	}

	renderer := blackfriday.NewHTMLRenderer(
		blackfriday.HTMLRendererParameters{
			Flags: blackfriday.CommonHTMLFlags &^ blackfriday.Smartypants,
		},
	)

	html := blackfriday.Run(md, blackfriday.WithRenderer(renderer))

	if err := os.MkdirAll(dst, os.ModePerm); err != nil {
		log.Error().Err(err).Msg("failed to create destination folder")
		return err
	}

	outputPath := filepath.Join(dst, replaceFileExtensionToHTML(filename))
	if len(templates) == 0 {
		if err := os.WriteFile(outputPath, html, os.ModePerm); err != nil {
			log.Error().Err(err).Msg("failed to write html file")
			return err
		}
		return nil
	}

	tmpl, err := template.ParseFiles(templates...)
	if err != nil {
		return err
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := tmpl.Execute(f, template.HTML(html)); err != nil {
		return err
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

func getFilename(path string) string {
	return filepath.Base(path)
}

func replaceFileExtensionToHTML(filename string) string {
	return trimMarkdownExtension(getFilename(filename)) + ".html"
}

func trimMarkdownExtension(filename string) string {
	if !isMarkdownFile(filename) {
		return filename
	}

	return strings.TrimSuffix(filename, filepath.Ext(filename))
}
