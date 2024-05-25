package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

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
		if cmd[0] == "exit" {
			var exitCode int
			if len(cmd) > 1 {
				exitCode, _ = strconv.Atoi(cmd[1])
			}
			os.Exit(exitCode)
		}
		fmt.Printf("%s: command not found\n", input)
	}
}
