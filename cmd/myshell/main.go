package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	builtins := []string{"exit", "echo", "type"}

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
		cmd := strings.Fields(input)
		switch cmd[0] {
		case "exit":
			var exitCode int
			if len(cmd) > 1 {
				exitCode, _ = strconv.Atoi(cmd[1])
			}
			os.Exit(exitCode)
		case "echo":
			fmt.Printf("%s\n", strings.Join(cmd[1:], " "))
		case "type":
			if slices.Contains(builtins, cmd[1]) {
				fmt.Printf("%s is a shell builtin\n", cmd[1])
			} else {
				fmt.Printf("%s not found\n", cmd[1])
			}
		default:
			fmt.Printf("%s: command not found\n", cmd[0])
		}
	}
}
