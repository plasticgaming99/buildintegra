package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/plasticgaming99/photon/modules/dyntypes"
)

var (
	packagename  = ""
	version      = 0
	release      = 1
	license      = ""
	architecture = ""
	description  = ""
	depends      = ""
	optional     = ""
	conflicts    = ""
	provides     = ""

	source = ""
)

func main() {
	fmt.Println("buildintegra")
	file, err := os.ReadFile("INTGBUILD")
	if err != nil {
		fmt.Println("File isn't exists, or broken.")
		os.Exit(1)
	}
	textf := string(file)
	textFile := strings.Split(textf, "\n")
	for i := 0; i < len(textFile); i++ {
		if strings.HasPrefix(textFile[i], "//") {
			continue
		}
		if strings.Contains(textFile[i], " = ") {
			maybevar := strings.Split(textFile[i], " = ")
			switch maybevar[0] {
			case "package":
				packagename = maybevar[1]
			case "version":
				version = dyntypes.DynInt(maybevar[1])
			case "release":
				release = dyntypes.DynInt(maybevar[1])
			case "license":
				license = dyntypes.DynInt(maybevar[1])
			case "architecture":
				license = maybevar[1]
			case "depends":
				depends = maybevar[1]
			case "optional":
				optional = maybevar[1]
			case "conflicts":
				depends = maybevar[1]
			case "provides":
				provides = maybevar[1]
			default:
				//not var!
			}
		}
	}
}

func executecmd(cmdname string, args ...string) {
	toexec := exec.Command(cmdname, args...)
	toexec.Stdin = os.Stdin
	toexec.Stdout = os.Stdout
	toexec.Stderr = os.Stderr
	toexec.Start()
	toexec.Wait()
}
