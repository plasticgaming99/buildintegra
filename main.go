package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

var (
	packagename  = "Example"
	version      = ""
	release      = 1
	license      = "Unknown"
	architecture = "x86_64"
	description  = "A package."
	url          = "https://example.org"
	depends      []string
	optdeps      []string
	builddeps    []string
	conflicts    []string
	provides     []string

	source []string

	downloader    = "aria2c"
	downloaderopt = "--auto-file-renaming=false"

	pkgdir string
	srcdir string
)

var intb = "= INTB =>"

func initBuildDir() {
	if _, err := os.Stat("source"); err != nil {
		os.Mkdir("source", os.ModePerm)
	}
}

func main() {
	fmt.Println("buildintegra")
	// start from 2, 2 for pkgname, 3 for pkgver, 4 for intgroot, 5 for pkgdir
	if strings.Contains(strings.Join(os.Args, ""), "PackageWithFakeroot") {
		packagename = os.Args[2]
		pkgdir = os.Args[5]
		version = os.Args[3]
		startpack(os.Args[4])
		os.Exit(0)
	}

	file, err := os.ReadFile("INTGBUILD")
	if err != nil {
		fmt.Println("File isn't exists, or broken.")
		os.Exit(1)
	}

	intgrootdir, err := os.Getwd()
	if err != nil {
		fmt.Println("internal error during getting root dir")
	}

	initBuildDir()

	os.Setenv("intgroot", intgrootdir)
	os.Setenv("srcdir", intgrootdir+"/source")
	srcdir = intgrootdir + "/source"
	os.Setenv("pkgdir", intgrootdir+"/package")
	pkgdir = intgrootdir + "/package"
	os.Setenv("pkgver", version)
	os.Setenv("pkgname", packagename)

	textf := string(file)
	textFile := strings.Split(textf, "\n")
	status := "setup"
	for i := 0; i < len(textFile); i++ {
		textFile[i] = strings.TrimSpace(textFile[i])
		if strings.HasPrefix(textFile[i], "//") {
			continue
		}
		if strings.Contains(textFile[i], "$") {
			textFile[i] = strings.ReplaceAll(textFile[i], "$pkgdir", pkgdir)
			textFile[i] = strings.ReplaceAll(textFile[i], "$srcdir", srcdir)
			textFile[i] = strings.ReplaceAll(textFile[i], "$intgroot", intgrootdir)
			textFile[i] = strings.ReplaceAll(textFile[i], "$pkgver", version)
		}
		if strings.Contains(textFile[i], " = ") && status == "setup" {
			maybevar := strings.Split(textFile[i], " = ")
			switch maybevar[0] {
			case "packagename":
				packagename = maybevar[1]
			case "version":
				version = maybevar[1]
			case "release":
				a, err := strconv.Atoi(maybevar[1])
				if err != nil {
					fmt.Println("release number is not int")
				}
				release = a
			case "license":
				license = maybevar[1]
			case "architecture":
				architecture = maybevar[1]
			case "description":
				description = maybevar[1]
			case "depends":
				depends = append(depends, maybevar[1])
			case "optdeps":
				optdeps = append(optdeps, maybevar[1])
			case "builddeps":
				builddeps = append(builddeps, maybevar[1])
			case "conflicts":
				depends = append(depends, maybevar[1])
			case "provides":
				provides = append(provides, maybevar[1])
			case "source":
				source = append(source, maybevar[1])
			default:
				//not var!
			}
		}

		if strings.Contains(textFile[i], "build:") {
			status = "build"
			fmt.Println(intb, "Start build...")
			os.Chdir(srcdir)
			// prep source
			for _, v := range source {
				fmt.Println("executed")
				if strings.Contains(v, ".git") {
					executecmd("git", "clone", v)
				} else {
					executecmd(downloader, downloaderopt, v)
				}
			}
			continue
		} else if strings.Contains(textFile[i], "package:") {
			os.RemoveAll(pkgdir)
			os.Chdir(pkgdir)
			status = "package"
			fmt.Println(intb, "Start packaging...")
			continue
		}

		if strings.HasPrefix(textFile[i], "cd") {
			os.Chdir(strings.Split(textFile[i], " ")[1])
		} else if strings.Contains(textFile[i], "export") {
			splitcmd := strings.SplitN(textFile[i], " ", 2)
			splittedvar := strings.SplitN(splitcmd[1], "=", 2)
			os.Setenv(splittedvar[0], splittedvar[1])
		} else if strings.HasPrefix(textFile[i], ":end") {
			switch strings.Split(textFile[i], " ")[1] {
			case "build":
				status = "buildfin"
				fmt.Println(intb, "Build Finished.")
			case "package":
				status = "packfin"
				os.WriteFile(pkgdir+"/.PACKAGE", []byte(generatePackInfo()), 0644)
				startpackwithfakeroot(intgrootdir)
			}
		} else if status == "build" || status == "package" {
			splitcmd := strings.Split(textFile[i], " ")
			err := executecmdwitherror(splitcmd[0], splitcmd[1:]...)
			if err != nil {
				log.Fatal(err)
			}
		}

	}
}

func executecmd(cmdname string, args ...string) {
	toexec := exec.Command(cmdname, args...)
	toexec.Stdin = os.Stdin
	toexec.Stdout = os.Stdout
	toexec.Stderr = os.Stderr
	toexec.Env = os.Environ()
	toexec.Start()
	toexec.Wait()
}

func executecmdwitherror(cmdname string, args ...string) error {
	toexec := exec.Command(cmdname, args...)
	toexec.Stdin = os.Stdin
	toexec.Stdout = os.Stdout
	toexec.Stderr = os.Stderr
	toexec.Env = os.Environ()
	return toexec.Run()
}

func executecmdwithstdinfile(infile io.Reader, cmdname string, args ...string) {
	toexec := exec.Command(cmdname, args...)
	toexec.Stdin = infile
	toexec.Stdout = os.Stdout
	toexec.Stderr = os.Stderr
	toexec.Env = os.Environ()
	toexec.Start()
	toexec.Wait()
}

func startpackwithfakeroot(intgroot string) {
	executecmd("fakeroot", os.Args[0], "PackageWithFakeroot", packagename, version, intgroot, pkgdir)
}

func startpack(intgroot string) {
	os.Chdir(pkgdir)
	archivename := intgroot + "/" + packagename + "-" + version + ".intg.tar.zst"
	executecmd("bsdtar", "-cvf", intgroot+"/"+packagename+"-"+version+".intg.tar.zst", ".",
		"--exclude", ".MTREE", ".PACKAGE",
	)
	archivefile, err := os.Open(archivename)
	if err != nil {
		log.Fatal("error during reading archive file")
	}
	defer archivefile.Close()

	archivereader := bufio.NewReader(archivefile)

	executecmdwithstdinfile(archivereader, "bsdtar", "-cf", ".MTREE",
		"--format=mtree", "--options", "!all,use-set,type,uid,gid,mode,time,size,sha256,link",
		"@-", "--exclude", ".MTREE", ".PACKAGE",
	)

	executecmd("bsdtar", "-cvf", intgroot+"/"+packagename+"-"+version+".intg.tar.zst", ".")

}

func appendStrings(s ...string) (ret []string) {
	ret = append(ret, s...)
	return
}

func formatMultiLineVar(name string, input []string) (output []string) {
	for _, str := range input {
		output = append(output, name+" = "+str)
	}
	return
}

func formatNewLine(strin []string) (ret string) {
	ret = strings.Join(strin, "\n")
	return
}

func generatePackInfo() (reText string) {
	{
		var txt []string
		apstr := appendStrings(
			"# generated with buildintegra with "+runtime.Version(),
			"package = "+packagename,
			"version = "+version,
			"release = "+strconv.Itoa(release),
			"license = "+license,
			"architecture = "+architecture,
			"description = "+description,
			"url = "+url,
			// depends
			// optdeps
			// conflicts
			// provides
		)
		txt = append(txt, apstr...)

		if len(depends) > 0 {
			txt = append(txt, formatMultiLineVar("depends", depends)...)
		}

		if len(optdeps) > 0 {
			txt = append(txt, formatMultiLineVar("optdeps", optdeps)...)
		}

		if len(conflicts) > 0 {
			txt = append(txt, formatMultiLineVar("conflicts", conflicts)...)
		}

		if len(provides) > 0 {
			txt = append(txt, formatMultiLineVar("provides", provides)...)
		}

		reText = formatNewLine(txt)
	}
	return
}
