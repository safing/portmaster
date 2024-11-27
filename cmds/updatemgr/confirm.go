package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func confirm(msg string) bool {
	fmt.Printf("%s: [y|n] ", msg)

	scanner := bufio.NewScanner(os.Stdin)
	ok := scanner.Scan()
	if ok && strings.TrimSpace(scanner.Text()) == "y" {
		return true
	}

	return false
}
