package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	builtins := []string{"exit", "echo", "type", "pwd", "cd"}

	for {
		fmt.Fprint(os.Stdout, "$ ")
		input, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		input = strings.TrimRight(input, "\n")
		cmd := SplitArgs(input)
		switch cmd[0] {
		case "exit":
			var exitCode int
			if len(cmd) > 1 {
				exitCode, _ = strconv.Atoi(cmd[1])
			}
			os.Exit(exitCode)
		case "echo":
			fmt.Printf("%s\n", strings.Join(cmd[1:], " "))
		case "pwd":
			wd, err := os.Getwd()
			if err != nil {
				panic(err)
			}
			fmt.Printf("%s\n", wd)
		case "cd":
			dir := cmd[1]
			if dir == "~" {
				homeEnv := os.Getenv("HOME")
				if homeEnv != "" {
					dir = homeEnv
				}
			}
			err := os.Chdir(dir)
			if err != nil {
				if _, isPathError := err.(*os.PathError); isPathError {
					fmt.Printf("cd: %s: No such file or directory\n", dir)
				} else {
					panic(err)
				}
			}
		case "type":
			if slices.Contains(builtins, cmd[1]) {
				fmt.Printf("%s is a shell builtin\n", cmd[1])
			} else {
				fullPath := searchPath(cmd[1])
				if fullPath == "" {
					fmt.Printf("%s: not found\n", cmd[1])
				} else {
					fmt.Printf("%s is %s\n", cmd[1], fullPath)
				}
			}
		default:
			fullPath := searchPath(cmd[0])
			// fmt.Fprintf(os.Stderr, "fullPath = %s\n", fullPath)
			if fullPath == "" {
				info, err := os.Stat(cmd[0])
				if err != nil || info.IsDir() {
					fmt.Printf("%s: command not found\n", cmd[0])
				} else {
					executeCmd(cmd[0], cmd)
				}
			} else {
				executeCmd(fullPath, cmd)
			}
		}
	}
}

// based on strings.FieldsFunc (but less efficient)
func SplitArgs(s string) []string {
	args := make([]string, 0)

	start := -1
	insideSingleQuote := false
	insideDoubleQuote := false
	hadSpaceBetweenQuotes := true
	for end, rune := range s {
		if insideDoubleQuote {
			if rune == '"' {
				if hadSpaceBetweenQuotes {
					args = append(args, s[start:end])
				} else {
					// just concatenate to previous string
					args[len(args)-1] += s[start:end]
				}
				start = -1
				insideDoubleQuote = false
				hadSpaceBetweenQuotes = false
			}
		} else if insideSingleQuote {
			if rune == '\'' {
				if hadSpaceBetweenQuotes {
					args = append(args, s[start:end])
				} else {
					// just concatenate to previous string
					args[len(args)-1] += s[start:end]
				}
				start = -1
				insideSingleQuote = false
				hadSpaceBetweenQuotes = false
			}
		} else if rune == '\'' {
			start = end + 1
			insideSingleQuote = true
		} else if rune == '"' {
			start = end + 1
			insideDoubleQuote = true
		} else if unicode.IsSpace(rune) {
			hadSpaceBetweenQuotes = true
			if start >= 0 {
				args = append(args, s[start:end])
				start = -1
			}
		} else {
			if start < 0 {
				start = end
			}
		}
	}

	// Last field might end at EOF.
	if start >= 0 {
		args = append(args, s[start:])
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

func executeCmd(cmdPath string, args []string) {
	cmd := exec.Cmd{}
	cmd.Path = cmdPath
	cmd.Args = args
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	// fmt.Println(cmd)
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
}
