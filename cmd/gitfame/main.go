//go:build !solution

package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"text/tabwriter"

	flag "github.com/spf13/pflag"

	"gitlab.com/slon/shad-go/gitfame/configs"

	"strings"
)

var (
	repository   = flag.String("repository", "", "путь до Git репозитория; по умолчанию текущая директория")
	revision     = flag.String("revision", "HEAD", "указатель на коммит; HEAD по умолчанию")
	orderBy      = flag.String("order-by", "lines", "ключ сортировки результатов; один из lines (дефолт), commits, files")
	useCommitter = flag.Bool("use-committer", false, "булев флаг, заменяющий в расчётах автора (дефолт) на коммиттера")
	format       = flag.String("format", "tabular", "формат вывода; один из tabular (дефолт), csv, json, json-lines")
	extensions   = flag.StringSlice("extensions", nil, "список расширений, сужающий список файлов в расчёте; множество ограничений разделяется запятыми, например, '.go,.md'")
	languages    = flag.StringSlice("languages", nil, "список языков (программирования, разметки и др.), сужающий список файлов в расчёте; множество ограничений разделяется запятыми, например 'go,markdown'")
	exclude      = flag.StringSlice("exclude", nil, "набор Glob паттернов, исключающих файлы из расчёта, например 'foo/*,bar/*'")
	restrictTo   = flag.StringSlice("restrict-to", nil, "набор Glob паттернов, исключающий все файлы, не удовлетворяющие ни одному из паттернов набора")
)

var sortKeys []string
var stats map[string]map[string]int
var commits, files map[string]map[string]bool
var sortIndex map[string]int

type Entry struct {
	Name    string `json:"name"`
	Lines   int    `json:"lines"`
	Commits int    `json:"commits"`
	Files   int    `json:"files"`
}

type LanguagesExtensions struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Extensions []string `json:"extensions"`
}

// var out io.Writer = os.Stdout // modified during testing

func main() {
	parseArgs()
	if err := gitfame(flag.Args()); err != nil {
		log.Printf("gitfame: %v\n", err)
		os.Exit(4)
	}
}

func parseArgs() {
	flag.Parse()

	if *repository != "" {
		repositoryExists, err := exists(path.Join(*repository, ".git"))
		if !repositoryExists || err != nil {
			log.Printf("parse error: '%s' is not a git repository\n", *repository)
			os.Exit(11)
		}
	}

	orderByFound := false
	sortKeys = []string{"lines", "commits", "files"}
	sortIndex = map[string]int{
		"lines":   1,
		"commits": 2,
		"files":   3}
	for i, key := range sortKeys {
		if key == *orderBy {
			sortKeys = append(sortKeys[:i], sortKeys[i+1:]...)

			sortKeys = append([]string{key}, sortKeys...)
			orderByFound = true
			break
		}
	}
	if !orderByFound {
		log.Printf("parse error: %s is not a sorting option\n", *orderBy)
		os.Exit(11)
	}

	formatOptions := map[string]bool{
		"tabular":    true,
		"csv":        true,
		"json":       true,
		"json-lines": true,
	}
	_, found := formatOptions[*format]
	if !found {
		log.Printf("parse error: %s is not a formatting option\n", *orderBy)
		os.Exit(11)
	}

}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
func gitfame(args []string) error {
	stats = make(map[string]map[string]int)
	commits = make(map[string]map[string]bool)
	files = make(map[string]map[string]bool)
	cmdString := fmt.Sprintf("git ls-tree -r %s", *revision)
	cmdArgs := strings.Split(cmdString, " ")
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = *repository
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}

	out, err := cmd.Output()
	if err != nil {
		log.Printf("error running command: %s output error %s\n", cmdString, cmd.Stderr)
		os.Exit(11)
	}

	outSplit := strings.Split(string(out), "\n")

	for _, line := range outSplit {
		if line == "" {
			break
		}
		lineSplit := strings.Fields(line)
		filePath, _ := strings.CutPrefix(line, strings.Join(lineSplit[:3], " "))
		filePath = strings.Trim(filePath, "	")
		lastCommitForFile := lineSplit[len(lineSplit)-2]

		if *restrictTo != nil {
			matchedOnce := false
			for _, pattern := range *restrictTo {
				matched, matchErr := filepath.Match(pattern, filePath)
				if matchErr != nil {
					panic("mathing fail")
				}
				if matched {
					matchedOnce = true
				}
			}
			if !matchedOnce {
				continue
			}
		}

		if *exclude != nil {
			matchedOnce := false
			for _, pattern := range *exclude {

				matched, matchErr := filepath.Match(pattern, filePath)
				if matchErr != nil {
					panic("mathing fail")
				}
				if matched {
					matchedOnce = true
				}
			}
			if matchedOnce {
				continue
			}
		}
		// log.Printf("before: %s", *extensions)
		if *languages != nil {
			var mappings []LanguagesExtensions
			err = json.Unmarshal(configs.LanguageExtensions, &mappings)

			// log.Printf("%s", mappings)
			if err != nil {
				log.Printf("unmarshal error %v\n", err)
				os.Exit(11)
			}

			for _, mapping := range mappings {

				for _, language := range *languages {
					// log.Printf("language=%s", language)
					// log.Printf("mapping.Name=%s", mapping.Name)

					if strings.ToLower(mapping.Name) == language {

						if *extensions == nil {
							*extensions = []string{}
						}
						*extensions = append(*extensions, mapping.Extensions...)
					}
				}
			}
		}
		// log.Printf("after: %s", *extensions)
		if *extensions != nil {
			matchedOnce := false
			for _, extension := range *extensions {
				matched := strings.HasSuffix(filePath, extension)
				if err != nil {
					panic("mathing fail")
				}
				if matched {
					// log.Printf("filepath=%s, pattern=%s", filePath, extension)
					matchedOnce = true
				}
			}
			if !matchedOnce {
				continue
			}
		}

		err = processFile(filePath, lastCommitForFile)
		if err != nil {
			log.Printf("error processing %s: %v\n", filePath, err)
			os.Exit(11)
		}
	}

	output := make([][]string, 0)
	delete(stats, "")
	for commiter, stat := range stats {
		lines, commits, files := strconv.Itoa(stat["lines"]), strconv.Itoa(stat["commits"]), strconv.Itoa(stat["files"])
		output = append(output, []string{commiter, lines, commits, files})
	}

	sort.Slice(output, func(i, j int) bool {
		iName, jName := output[i][0], output[j][0]
		i0, _ := strconv.Atoi(output[i][sortIndex[sortKeys[0]]])
		j0, _ := strconv.Atoi(output[j][sortIndex[sortKeys[0]]])
		i1, _ := strconv.Atoi(output[i][sortIndex[sortKeys[1]]])
		j1, _ := strconv.Atoi(output[j][sortIndex[sortKeys[1]]])
		i2, _ := strconv.Atoi(output[i][sortIndex[sortKeys[2]]])
		j2, _ := strconv.Atoi(output[j][sortIndex[sortKeys[2]]])

		// log.Printf("priority: %d %d %d", sortIndex[sortKeys[0]], sortIndex[sortKeys[1]], sortIndex[sortKeys[2]])

		return !(i0 < j0 || (i0 == j0 && i1 < j1) || (i0 == j0 && i1 == j1 && i2 < j2) || (i0 == j0 && i1 == j1 && i2 == j2 && iName > jName))
	})

	switch *format {
	case "tabular":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		fmt.Fprintln(w, "Name\tLines\tCommits\tFiles")
		for _, entry := range output {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", entry[0], entry[1], entry[2], entry[3])
		}
		w.Flush()
	case "csv":
		// b := new(bytes.Buffer)
		w := csv.NewWriter(os.Stdout)
		err = w.Write([]string{"Name", "Lines", "Commits", "Files"})
		if err != nil {
			log.Fatal(err)
		}
		err = w.WriteAll(output)
		if err != nil {
			log.Fatal(err)
		}

	case "json":
		jsonOuptput := []Entry{}
		for _, slice := range output {
			lines, _ := strconv.Atoi(slice[1])
			commits, _ := strconv.Atoi(slice[2])
			files, _ := strconv.Atoi(slice[3])
			entry := Entry{Name: slice[0], Lines: lines, Commits: commits, Files: files}

			jsonOuptput = append(jsonOuptput, entry)
		}
		// log.Print(jsonOuptput)
		marshalled, _ := json.Marshal(jsonOuptput)
		fmt.Printf("%s", marshalled)
	case "json-lines":
		for _, slice := range output {
			lines, _ := strconv.Atoi(slice[1])
			commits, _ := strconv.Atoi(slice[2])
			files, _ := strconv.Atoi(slice[3])
			entry := Entry{Name: slice[0], Lines: lines, Commits: commits, Files: files}
			marshalled, _ := json.Marshal(entry)
			fmt.Printf("%s\n", marshalled)
		}
	}

	return nil
}
func processEmptyFile(filePath, lastCommitForFile string) error {
	cmdString := fmt.Sprintf("git log %s -- %s", *revision, filePath)
	cmdArgs := strings.Fields(cmdString)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	if *repository != "" {
		cmd.Dir = *repository
	}
	// may cause stupid shit
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}

	out, err := cmd.Output()
	if err != nil {
		// log.Printf("error running command '%s':  output error: %s\n", cmdString, cmd.Stderr)
		return nil
	}
	outSplit := strings.Split(string(out), "\n")
	commitHash := strings.Fields(outSplit[0])[1]
	commitAuthor, _ := strings.CutPrefix(outSplit[1], "Author: ")
	authorSplit := strings.Fields(outSplit[1])
	suffix := authorSplit[len(authorSplit)-1]
	commitAuthor, _ = strings.CutSuffix(commitAuthor, " "+suffix)
	_, statExists := stats[commitAuthor]
	if !statExists {
		stats[commitAuthor] = make(map[string]int)
	}
	stats[commitAuthor]["files"]++

	_, authorExists := commits[commitAuthor]
	if !authorExists {
		commits[commitAuthor] = make(map[string]bool)
	}
	_, commitExists := commits[commitAuthor][commitHash]
	if !commitExists {
		commits[commitAuthor][commitHash] = true
		stats[commitAuthor]["commits"]++
	}

	return nil
}
func processFile(filePath, lastCommitForFile string) error {
	cmdArgs := []string{"blame", "--porcelain", *revision, filePath}
	cmd := exec.Command("git", cmdArgs...)
	if *repository != "" {
		cmd.Dir = *repository
	}
	// may cause stupid shit
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil
	}

	out, err := cmd.Output()
	if err != nil {
		log.Printf("error running command[%s]:  output error: %s\n", cmdArgs, cmd.Stderr)
		os.Exit(11)
	}
	if string(out) == "" {
		return processEmptyFile(filePath, lastCommitForFile)
	}
	// log.Printf("string(out)='%s'", string(out))
	outSplit := strings.Split(string(out), "\n")

	linesUntilHeader := 0
	var commitAuthor, commitHash string
	successiveLines := 0

	commitAuthors := make(map[string]string)

	for i, line := range outSplit {

		// log.Printf("line %d/%d, linesUntilHeader %d, author %s, hash %s: '%s'", i, len(outSplit), linesUntilHeader, commitAuthor, commitHash, line)

		if strings.HasPrefix(line, "	") {
			linesUntilHeader--
		} else {
			words := strings.Fields(line)
			if line != "" {
				switch words[0] {

				case "author":
					if !*useCommitter {
						_, commitAuthor, _ = strings.Cut(line, " ")
						commitAuthors[commitHash] = commitAuthor
					}
				case "committer":
					if *useCommitter {
						_, commitAuthor, _ = strings.Cut(line, " ")
						commitAuthors[commitHash] = commitAuthor
					}
				}
			}

			if linesUntilHeader == 0 {

				_, statExists := stats[commitAuthor]
				if !statExists {
					stats[commitAuthor] = make(map[string]int)
				}
				stats[commitAuthor]["lines"] += successiveLines

				_, commitAuthorExists := commits[commitAuthor]
				if !commitAuthorExists {
					commits[commitAuthor] = make(map[string]bool)
				}
				_, commitExists := commits[commitAuthor][commitHash]
				if !commitExists {
					commits[commitAuthor][commitHash] = true
					stats[commitAuthor]["commits"]++
				}

				_, fileAuthorExists := files[commitAuthor]
				if !fileAuthorExists {
					files[commitAuthor] = make(map[string]bool)
				}
				_, fileExists := files[commitAuthor][filePath]
				if !fileExists {
					files[commitAuthor][filePath] = true
					stats[commitAuthor]["files"]++
				}

				if i != len(outSplit)-1 {
					commitHash = words[0]
					commitAuthor = commitAuthors[commitHash]
					if len(words) == 4 {
						successiveLines, _ = strconv.Atoi(words[3])
					}
					linesUntilHeader = successiveLines
				}

			}
		}

	}
	return nil
}
