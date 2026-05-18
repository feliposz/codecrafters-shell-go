package main

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/chzyer/readline"
)

var history []string
var histFilePath string
var lastAppended int

func main() {
	completer := readline.NewPrefixCompleter(
		readline.PcItem("echo"),
		readline.PcItem("exit"),
		readline.PcItem("type"),
		readline.PcItem("pwd"),
		readline.PcItem("cd"),
		readline.PcItem("history"),
		readline.PcItem("jobs"),
		readline.PcItemDynamic(listPathCompleter, nil),
	)

	histFilePath = os.Getenv("HISTFILE")
	if len(histFilePath) > 0 {
		readHistory(histFilePath, os.Stderr)
	}

	reader, err := readline.NewEx(&readline.Config{
		Prompt:          "$ ",
		AutoComplete:    &completerWithBells{completer, 0},
		InterruptPrompt: "^C",
	})
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	reader.CaptureExitSignal()

	for {
		jobList.print(true)
		input, err := reader.Readline()
		if err == readline.ErrInterrupt {
			if len(input) == 0 {
				break
			} else {
				continue
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		input = strings.TrimSpace(input)

		if len(input) == 0 {
			continue
		}

		history = append(history, input)

		args := splitTokens(input)

		isBackground := args[len(args)-1] == "&"
		if isBackground {
			args = args[0 : len(args)-1]
		}

		segments := splitPipeline(args)

		if len(segments) == 1 {
			args = segments[0]
			args, inFile, outFile, errFile, err := handleRedirections(args, os.Stdin, os.Stdout, os.Stdout)
			if err != nil {
				panic(err)
			}
			handleCommand(args, isBackground, inFile, outFile, errFile, nil)
		} else {
			if isBackground {
				panic("back ground jobs with pipes are not supported at the moment")
			}
			inFile, outFile, errFile := os.Stdin, os.Stdout, os.Stdout
			var wg sync.WaitGroup
			var input, previousInput io.ReadCloser
			var output io.WriteCloser
			previousInput = inFile
			for i, segment := range segments {
				wg.Add(1)
				if i < len(segments)-1 {
					input, output = io.Pipe()
				} else {
					output = outFile
				}
				// TODO: should handle redirections here
				go handleCommand(segment, false, previousInput, output, errFile, &wg)
				previousInput = input
			}
			wg.Wait()
		}
	}

	if len(histFilePath) > 0 {
		writeHistory(histFilePath, os.Stderr)
	}
}

func splitPipeline(args []string) [][]string {
	if len(args) == 0 {
		return nil
	}
	segments := make([][]string, 0)
	for len(args) > 0 {
		pipeIndex := slices.Index(args, "|")
		if pipeIndex == -1 {
			segments = append(segments, args)
			break
		} else {
			segments = append(segments, args[:pipeIndex])
			args = args[pipeIndex+1:]
		}
	}
	return segments
}

func handleRedirections(args []string, inFile, outFile, errFile *os.File) (resultArgs []string, stdinFile, stdoutFile, stderrFile *os.File, err error) {

	firstRedirectIndex := len(args)
	stdinFile, stdoutFile, stderrFile = inFile, outFile, errFile

	stdoutRedirectIndex := slices.Index(args, "1>")
	if stdoutRedirectIndex == -1 {
		stdoutRedirectIndex = slices.Index(args, ">")
	}
	if stdoutRedirectIndex != -1 {
		stdoutFilePath := args[stdoutRedirectIndex+1]
		firstRedirectIndex = min(firstRedirectIndex, stdoutRedirectIndex)
		if stdoutFile, err = os.Create(stdoutFilePath); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	stdoutAppendIndex := slices.Index(args, "1>>")
	if stdoutAppendIndex == -1 {
		stdoutAppendIndex = slices.Index(args, ">>")
	}
	if stdoutAppendIndex != -1 {
		stdoutFilePath := args[stdoutAppendIndex+1]
		firstRedirectIndex = min(firstRedirectIndex, stdoutAppendIndex)
		if stdoutFile, err = os.OpenFile(stdoutFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	stderrRedirectIndex := slices.Index(args, "2>")
	if stderrRedirectIndex != -1 {
		stderrFilePath := args[stderrRedirectIndex+1]
		firstRedirectIndex = min(firstRedirectIndex, stderrRedirectIndex)
		if stderrFile, err = os.Create(stderrFilePath); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	stderrAppendIndex := slices.Index(args, "2>>")
	if stderrAppendIndex != -1 {
		stderrFilePath := args[stderrAppendIndex+1]
		firstRedirectIndex = min(firstRedirectIndex, stderrAppendIndex)
		if stderrFile, err = os.OpenFile(stderrFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	resultArgs = args[:firstRedirectIndex]
	return
}

// wrap a completer and implement bell
type completerWithBells struct {
	inner         readline.AutoCompleter
	tabPressCount int
}

func (c *completerWithBells) Do(line []rune, pos int) ([][]rune, int) {
	items, offset := c.inner.Do(line, pos)
	// inner completer returned no matches, sound a bell
	if len(items) == 0 {
		fmt.Fprintf(os.Stderr, "\a")
	}
	if len(items) > 1 {
		items = uniqueAndSorted(items)
	}
	// returned many matches
	if len(items) > 1 {
		commonPrefix := getCommonPrefix(items)
		if len(commonPrefix) > 0 {
			// if there is a common prefix, let readline complete it (default behavior)
			return items, offset
		}

		// default handling of readline library is different, so we'll do it as codecrafters want
		if c.tabPressCount == 0 {
			fmt.Fprintf(os.Stderr, "\a")
			c.tabPressCount++
		} else {
			fmt.Println()
			for i, item := range items {
				if i > 0 {
					fmt.Print(" ")
				}
				fmt.Print(string(line) + string(item))
			}
			c.tabPressCount = 0
			fmt.Println()
			fmt.Print("$ " + string(line))
		}
		return nil, 0
	}
	return items, offset
}

// assume entries are unique and sorted
func getCommonPrefix(suggestions [][]rune) []rune {
	first, last := suggestions[0], suggestions[len(suggestions)-1]
	result := first[:0]
	for i := 0; i < len(first) && i < len(last) && first[i] == last[i]; i++ {
		result = first[:i+1]
	}
	return result
}

func uniqueAndSorted(items [][]rune) [][]rune {
	if len(items) < 2 {
		return items
	}
	slices.SortFunc(items, slices.Compare)
	result := make([][]rune, 0, len(items))
	result = append(result, items[0])
	for i := 1; i < len(items); i++ {
		if slices.Compare(items[i], items[i-1]) != 0 {
			result = append(result, items[i])
		}
	}
	return result
}

var shellVariables = map[string]*string{}
var shellVariableValidator = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")

func isValidVarName(name string) bool {
	return shellVariableValidator.MatchString(name)
}

func handleCommand(args []string, isBackground bool, stdin io.ReadCloser, stdout, stderr io.WriteCloser, wg *sync.WaitGroup) {

	builtins := []string{
		"exit",
		"echo",
		"type",
		"pwd",
		"cd",
		"history",
		"jobs",
		"declare",
	}

	switch args[0] {
	case "exit":
		var exitCode int
		if len(args) > 1 {
			exitCode, _ = strconv.Atoi(args[1])
		}
		if len(histFilePath) > 0 {
			writeHistory(histFilePath, stderr)
		}
		os.Exit(exitCode)
	case "echo":
		fmt.Fprintf(stdout, "%s\n", strings.Join(args[1:], " "))
	case "pwd":
		wd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(stdout, "%s\n", wd)
	case "cd":
		dir := args[1]
		if dir == "~" {
			homeEnv := os.Getenv("HOME")
			if homeEnv != "" {
				dir = homeEnv
			}
		}
		err := os.Chdir(dir)
		if err != nil {
			if _, isPathError := err.(*os.PathError); isPathError {
				fmt.Fprintf(stderr, "cd: %s: No such file or directory\n", dir)
			} else {
				panic(err)
			}
		}
	case "type":
		if slices.Contains(builtins, args[1]) {
			fmt.Fprintf(stdout, "%s is a shell builtin\n", args[1])
		} else {
			fullPath := searchPath(args[1])
			if fullPath == "" {
				fmt.Fprintf(stderr, "%s: not found\n", args[1])
			} else {
				fmt.Fprintf(stdout, "%s is %s\n", args[1], fullPath)
			}
		}
	case "history":
		if len(args) > 2 && args[1] == "-r" {
			readHistory(args[2], stderr)
		} else if len(args) > 2 && args[1] == "-w" {
			writeHistory(args[2], stderr)
		} else if len(args) > 2 && args[1] == "-a" {
			if file, err := os.OpenFile(args[2], os.O_RDWR|os.O_APPEND, 0644); err != nil {
				fmt.Fprintf(stderr, "history: cannot append to %s\n", args[2])
			} else {
				for _, cmd := range history[lastAppended:] {
					fmt.Fprintln(file, cmd)
				}
				file.Close()
				lastAppended = len(history)
			}
		} else {
			start := 0
			if len(args) > 1 {
				n, err := strconv.Atoi(args[1])
				if err != nil {
					fmt.Fprintf(stderr, "history: %s: numeric argument required\n", args[1])
				}
				start = max(len(history)-n, 0)
			}
			for i, entry := range history[start:] {
				fmt.Fprintf(stdout, "%4d  %s\n", start+i+1, entry)
			}
		}
	case "jobs":
		jobList.print(false)
	case "declare":
		if args[1] == "-p" {
			if value, ok := shellVariables[args[2]]; ok {
				if value != nil {
					fmt.Printf("declare -- %s=\"%s\"\n", args[2], *value)
				} else {
					fmt.Printf("declare -- %s\n", args[2])
				}
			} else {
				fmt.Printf("declare: %s: not found\n", args[2])
			}
		} else if len(args) > 1 {
			for _, varDecl := range args[1:] {
				parts := strings.SplitN(varDecl, "=", 2)
				if isValidVarName(parts[0]) {
					if len(parts) == 2 {
						shellVariables[parts[0]] = &parts[1]
					} else {
						shellVariables[parts[0]] = nil
					}
				} else {
					fmt.Printf("declare: `%s': not a valid identifier\n", varDecl)
				}

			}
		}
	default:
		fullPath := searchPath(args[0])
		// fmt.Fprintf(os.Stderr, "fullPath = %s\n", fullPath)
		if fullPath == "" {
			info, err := os.Stat(args[0])
			if err != nil || info.IsDir() {
				fmt.Fprintf(stderr, "%s: command not found\n", args[0])
			} else {
				executeCmd(args[0], args, isBackground, stdin, stdout, stderr)
			}
		} else {
			executeCmd(fullPath, args, isBackground, stdin, stdout, stderr)
		}
	}

	for _, file := range []io.Closer{stdin, stdout, stderr} {
		if file != os.Stdin && file != os.Stdout && file != os.Stderr {
			file.Close()
		}
	}
	if wg != nil {
		wg.Done()
	}
}

// based on strings.FieldsFunc (but less efficient)
func splitTokens(s string) []string {
	args := make([]string, 0)

	var current strings.Builder

	insideSingleQuote := false
	insideDoubleQuote := false
	hadSpaceBetweenQuotes := true
	backslash := false

	for _, rune := range s {
		if insideDoubleQuote {
			if !backslash && rune == '"' {
				if hadSpaceBetweenQuotes {
					args = append(args, current.String())
				} else {
					// just concatenate to previous string
					args[len(args)-1] += current.String()
				}
				current.Reset()
				insideDoubleQuote = false
				hadSpaceBetweenQuotes = false
			} else if !backslash && rune == '\\' {
				backslash = true
			} else if backslash {
				switch rune {
				case '\\', '"', '$', '\n':
					backslash = false
					current.WriteRune(rune)
				default:
					backslash = false
					current.WriteRune('\\')
					current.WriteRune(rune)
				}
			} else {
				current.WriteRune(rune)
			}
		} else if insideSingleQuote {
			if rune == '\'' {
				if hadSpaceBetweenQuotes {
					args = append(args, current.String())
				} else {
					// just concatenate to previous string
					args[len(args)-1] += current.String()
				}
				current.Reset()
				insideSingleQuote = false
				hadSpaceBetweenQuotes = false
			} else {
				current.WriteRune(rune)
			}
		} else if backslash {
			backslash = false
			current.WriteRune(rune)
		} else if rune == '\\' {
			backslash = true
		} else if rune == '\'' {
			insideSingleQuote = true
		} else if rune == '"' {
			insideDoubleQuote = true
		} else if unicode.IsSpace(rune) {
			hadSpaceBetweenQuotes = true
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(rune)
		}
	}

	// Last field might end at EOF.
	if current.Len() > 0 {
		if hadSpaceBetweenQuotes {
			args = append(args, current.String())
		} else {
			// just concatenate to previous string
			args[len(args)-1] += current.String()
		}
	}

	return args
}

// cache the result of previous search
var previousPrefix = ""
var previousSuggestions = []string{}

func listPathCompleter(prefix string) []string {
	pathEnv := os.Getenv("PATH")
	if prefix == previousPrefix {
		return previousSuggestions
	}
	pathDirs := strings.Split(pathEnv, ":")
	suggestions := make([]string, 0)
	for _, dir := range pathDirs {
		files, _ := os.ReadDir(dir)
		for _, file := range files {
			name := file.Name()
			suggestions = append(suggestions, name)
		}
	}
	previousPrefix = prefix
	previousSuggestions = suggestions
	return suggestions
}

func searchPath(name string) string {
	pathEnv := os.Getenv("PATH")
	pathDirs := strings.Split(pathEnv, ":")
	// fmt.Fprintf(os.Stderr, "pathDirs = %v\n", pathDirs)
	for _, dir := range pathDirs {
		dir = strings.TrimSuffix(dir, "/")
		fullPath := fmt.Sprintf("%s/%s", dir, name)
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0 {
			return fullPath
		}
	}
	return ""
}

func executeCmd(cmdPath string, args []string, isBackground bool, stdin io.Reader, stdout, stderr io.Writer) {
	cmd := exec.Cmd{}
	cmd.Path = cmdPath
	cmd.Args = args
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	cmd.Stdin = stdin
	if isBackground {
		cmd.Start()
		jobId := jobList.add(&cmd, strings.Join(args, " "))
		fmt.Printf("[%d] %d\n", jobId, cmd.Process.Pid)
		// reap the child process when it exits
		go cmd.Wait()
	} else {
		cmd.Run()
	}
}

func readHistory(path string, stderr io.WriteCloser) {
	if file, err := os.Open(path); err != nil {
		fmt.Fprintf(stderr, "history: cannot read %s\n", path)
	} else {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			history = append(history, scanner.Text())
		}
		file.Close()
	}
}

func writeHistory(path string, stderr io.WriteCloser) {
	if file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644); err != nil {
		fmt.Fprintf(stderr, "history: cannot write %s\n", path)
	} else {
		for _, cmd := range history {
			fmt.Fprintln(file, cmd)
		}
		file.Close()
	}
}

type Job struct {
	jobId   int
	cmdLine string
	cmd     *exec.Cmd
	done    bool
}

type JobList struct {
	list []*Job
}

var jobList = JobList{}

func (jobs *JobList) add(cmd *exec.Cmd, cmdLine string) int {
	jobId := jobList.nextJobId()
	jobs.list = append(jobs.list, &Job{jobId, cmdLine, cmd, false})
	return jobId
}

func (jobs JobList) nextJobId() int {
	jobId := 1
	for _, job := range jobs.list {
		if job.jobId == jobId {
			jobId++
		}
	}
	return jobId
}

func (jobs *JobList) print(onlyDone bool) {
	slices.SortFunc(jobs.list, func(a, b *Job) int {
		return cmp.Compare(a.jobId, b.jobId)
	})
	numJobs := len(jobs.list)
	for i, job := range jobs.list {
		marker := ' '
		switch i {
		case numJobs - 1:
			marker = '+'
		case numJobs - 2:
			marker = '-'
		}
		stateDescription := "Running"
		if job.cmd.ProcessState != nil && job.cmd.ProcessState.Exited() {
			stateDescription = "Done"
			job.done = true
		}
		if onlyDone && !job.done {
			continue
		}
		fmt.Printf("[%d]%c  %-24s %s\n", job.jobId, marker, stateDescription, job.cmdLine)
	}
	jobs.list = slices.DeleteFunc(jobs.list, func(job *Job) bool {
		return job.done
	})
}
