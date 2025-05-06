package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/chzyer/readline"
)

func main() {
	completer := readline.NewPrefixCompleter(
		readline.PcItem("echo"),
		readline.PcItem("exit"),
	)

	reader, err := readline.NewEx(&readline.Config{
		Prompt:          "$ ",
		AutoComplete:    &bellOnNoMatchCompleter{completer},
		InterruptPrompt: "^C",
	})
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	reader.CaptureExitSignal()

	for {
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

		args := splitArgs(input)
		firstRedirectIndex := len(args)

		stdinFile, stdoutFile, stderrFile := os.Stdin, os.Stdout, os.Stdout

		stdoutRedirectIndex := slices.Index(args, "1>")
		if stdoutRedirectIndex == -1 {
			stdoutRedirectIndex = slices.Index(args, ">")
		}
		if stdoutRedirectIndex != -1 {
			stdoutFilePath := args[stdoutRedirectIndex+1]
			firstRedirectIndex = min(firstRedirectIndex, stdoutRedirectIndex)
			if stdoutFile, err = os.Create(stdoutFilePath); err != nil {
				panic(err)
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
				panic(err)
			}
		}

		stderrRedirectIndex := slices.Index(args, "2>")
		if stderrRedirectIndex != -1 {
			stderrFilePath := args[stderrRedirectIndex+1]
			firstRedirectIndex = min(firstRedirectIndex, stderrRedirectIndex)
			if stderrFile, err = os.Create(stderrFilePath); err != nil {
				panic(err)
			}
		}

		stderrAppendIndex := slices.Index(args, "2>>")
		if stderrAppendIndex != -1 {
			stderrFilePath := args[stderrAppendIndex+1]
			firstRedirectIndex = min(firstRedirectIndex, stderrAppendIndex)
			if stderrFile, err = os.OpenFile(stderrFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err != nil {
				panic(err)
			}
		}

		args = args[:firstRedirectIndex]
		handleCommand(args, stdinFile, stdoutFile, stderrFile)
		if stdoutFile != os.Stdout {
			stdoutFile.Close()
		}
		if stderrFile != os.Stdout {
			stderrFile.Close()
		}
	}
}

// wrap a completer and implement bell
type bellOnNoMatchCompleter struct {
	inner readline.AutoCompleter
}

func (c *bellOnNoMatchCompleter) Do(line []rune, pos int) ([][]rune, int) {
	items, offset := c.inner.Do(line, pos)
	// inner completer returned no matches, sound a bell
	if len(items) == 0 {
		fmt.Fprintf(os.Stderr, "\a")
	}
	return items, offset
}

func handleCommand(args []string, stdin, stdout, stderr *os.File) {

	builtins := []string{"exit", "echo", "type", "pwd", "cd"}

	switch args[0] {
	case "exit":
		var exitCode int
		if len(args) > 1 {
			exitCode, _ = strconv.Atoi(args[1])
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
	default:
		fullPath := searchPath(args[0])
		// fmt.Fprintf(os.Stderr, "fullPath = %s\n", fullPath)
		if fullPath == "" {
			info, err := os.Stat(args[0])
			if err != nil || info.IsDir() {
				fmt.Fprintf(stderr, "%s: command not found\n", args[0])
			} else {
				executeCmd(args[0], args, stdin, stdout, stderr)
			}
		} else {
			executeCmd(fullPath, args, stdin, stdout, stderr)
		}
	}
}

// based on strings.FieldsFunc (but less efficient)
func splitArgs(s string) []string {
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

func searchPath(name string) string {
	pathEnv := os.Getenv("PATH")
	pathDirs := strings.Split(pathEnv, ":")
	// fmt.Fprintf(os.Stderr, "pathDirs = %v\n", pathDirs)
	for _, dir := range pathDirs {
		dir = strings.TrimSuffix(dir, "/")
		fullPath := fmt.Sprintf("%s/%s", dir, name)
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			return fullPath
		}
	}
	return ""
}

func executeCmd(cmdPath string, args []string, stdin, stdout, stderr *os.File) {
	cmd := exec.Cmd{}
	cmd.Path = cmdPath
	cmd.Args = args
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	cmd.Stdin = stdin
	// fmt.Println(cmd)
	cmd.Run()
	// err := cmd.Run()
	// if err != nil {
	// 	panic(err)
	// }
}
