package main

import (
	"encoding/json"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
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
				Usage:       "Source folder containing markdown files",
				EnvVars:     []string{"APP_SOURCE_FOLDER"},
				DefaultText: "posts",
				Value:       "posts",
			},
			&cli.StringFlag{
				Name:        "destination",
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
			&cli.StringFlag{
				Name:        "index",
				Usage:       "A index file to track generated markdown files",
				EnvVars:     []string{"APP_INDEX_FILE"},
				DefaultText: "_index",
				Value:       "_index",
			},
			&cli.BoolFlag{
				Name:        "auto-commit",
				Usage:       "Automatically commit changes to git repository",
				EnvVars:     []string{"APP_AUTO_COMMIT"},
				DefaultText: "false",
				Value:       false,
			},
			&cli.StringFlag{
				Name:        "github-token",
				Usage:       "Github token to push changes",
				EnvVars:     []string{"GITHUB_TOKEN"},
				Required:    true,
				Destination: &GITHUB_TOKEN,
			},
			&cli.StringFlag{
				Name:        "github-username",
				Usage:       "Github username to push changes",
				EnvVars:     []string{"GITHUB_USERNAME"},
				Required:    true,
				Destination: &GITHUB_USERNAME,
			},
			&cli.StringFlag{
				Name: "git-user-name",
				Usage: "Git user.name to commit changes. " +
					"Default is 'md2html', which is this App's name",
				EnvVars:     []string{"APP_GIT_USER_NAME"},
				DefaultText: "md2html",
				Value:       "md2html",
			},
			&cli.StringFlag{
				Name: "git-user-email",
				Usage: "Git user.email to commit changes. " +
					"Default is 'md2html', which is this App's name",
				EnvVars:     []string{"APP_GIT_USER_EMAIL"},
				DefaultText: "md2html",
				Value:       "md2html",
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("failed to run app")
	}
}

type tracking struct {
	Files  []string `json:"files"`
	Commit string   `json:"__commit__"`
}

var (
	GITHUB_TOKEN    string
	GITHUB_USERNAME string
)

func run(c *cli.Context) error {
	pathToSource := c.String("source")
	pathToDestination := c.String("destination")
	pathToTemplates := c.StringSlice("templates")
	pathToIndex := c.String("index")

	gitUsername := c.String("git-username")
	gitUserEmail := c.String("git-user-email")

	track, err := loadTrackingIndex(pathToIndex)
	if err != nil {
		log.Error().Err(err).Msg("failed to load tracking index")
		return err
	}

	diffFiles, err := getChangedFiles(track.Commit)
	if err != nil {
		log.Error().Err(err).Msg("failed to get changed files")
		return err
	}

	entries, err := os.ReadDir(pathToSource)
	if err != nil {
		log.Error().Err(err).Msg("failed to read source folder")
		return err
	}

	hasAnyChanges := false
	for _, entry := range entries {
		filename := entry.Name()

		isChangedFile := inSlice(filename, diffFiles)
		if len(diffFiles) > 0 && !isChangedFile {
			if !inSlice(filename, diffFiles) {
				continue
			}
		}

		if !isChangedFile && inSlice(filename, track.Files) {
			log.Info().Str("filename", filename).Msg("skip already converted markdown file")
			continue
		}

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

		hasAnyChanges = true
		track.Files = append(track.Files, filename)
	}

	if !hasAnyChanges {
		log.Info().Msg("no file was generated")
		return nil
	}

	if err := createFileIfNotExists(pathToIndex); err != nil {
		log.Error().Err(err).Msg("failed to create index file")
		return err
	}

	hash, err := commitAndPushChanges(
		gitUsername,
		gitUserEmail,
		pathToDestination,
		"generated new markdown files",
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to commit and push changes")
		return err
	}

	track.Commit = hash

	updatedIndex, err := json.Marshal(track)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal index file")
		return err
	}
	if err := os.WriteFile(pathToIndex, updatedIndex, os.ModePerm); err != nil {
		log.Error().Err(err).Msg("failed to write index file")
		return err
	}

	if _, err := commitAndPushChanges(
		gitUsername,
		gitUserEmail,
		"_index",
		"update tracking index",
	); err != nil {
		log.Error().Err(err).Msg("failed to commit changes")
		return err
	}

	return nil
}

func loadTrackingIndex(pathToIndex string) (*tracking, error) {
	track := &tracking{}
	if fileExists(pathToIndex) {
		index, err := os.ReadFile(pathToIndex)
		if err != nil {
			log.Error().Err(err).Msg("Failed to read index file")
			return nil, err
		}
		json.Unmarshal(index, track)
	}

	return track, nil
}

func getChangedFiles(lastCommit string) ([]string, error) {
	if lastCommit == "" {
		log.Info().Msg("no last commit found")
		return nil, nil
	}

	output, err := exec.Command("git", "diff", "--name-only", "-z", lastCommit).CombinedOutput()
	if err != nil {
		log.Error().Err(err).Msgf("Failed to run git diff command: %s", string(output))
		return nil, err
	}

	diffs := strings.Split(string(output), "\x00")

	changedFiles := make([]string, 0)
	for _, diff := range diffs {
		if isMarkdownFile(diff) {
			changedFiles = append(changedFiles, filepath.Base(diff))
		}
	}

	return changedFiles, nil
}

func commitAndPushChanges(user, email, glob, message string) (hash string, err error) {
	repo, err := git.PlainOpen(".")
	if err != nil {
		log.Error().Err(err).Msg("failed to open git repository")
		return "", err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		log.Error().Err(err).Msg("failed to get worktree")
		return "", err
	}

	if err := worktree.AddGlob(glob); err != nil {
		log.Error().Err(err).Msg("failed to add changes")
		return "", err
	}

	commit, err := worktree.Commit(
		message,
		&git.CommitOptions{
			Author: &object.Signature{
				Name:  user,
				Email: email,
				When:  time.Now(),
			},
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to commit changes")
		return "", err
	}

	if err := repo.Push(
		&git.PushOptions{
			Auth: &http.BasicAuth{
				Username: GITHUB_USERNAME,
				Password: GITHUB_TOKEN,
			},
		},
	); err != nil {
		log.Error().Err(err).Msg("failed to push changes")
		return "", err
	}

	return commit.String(), nil

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

func createFileIfNotExists(pathToFile string) error {
	if fileExists(pathToFile) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(pathToFile), os.ModePerm); err != nil {
		return err
	}

	if _, err := os.Create(pathToFile); err != nil {
		return err
	}

	return nil
}

func fileExists(pathToFile string) bool {
	info, err := os.Stat(pathToFile)
	if os.IsNotExist(err) {
		return false
	}

	return !info.IsDir()
}

func inSlice(needle string, haystack []string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
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
