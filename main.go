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
	packagename  []string
	version      = ""
	release      = 1
	license      = "Unknown"
	architecture = "x86_64"
	description  = "A package."
	url          = ""
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

	fakeroot          = false
	fakerootToPackage = ""
)

var intb = "= INTB =>"

func initBuildDir() {
	if _, err := os.Stat("source"); err != nil {
		os.Mkdir("source", os.ModePerm)
	}
}

// Real init that read BuildIntegra configuration.
func init() {
	_, err := os.Stat("/etc/bintegra.conf")
	if err != nil {
		return
	}

	file, err := os.ReadFile("/etc/bintegra.conf")
	if err != nil {
		fmt.Println(intb, "Error reading configuration")
		return
	}
	configfile := strings.Split(string(file), "\n")
	file = nil
	for i := 0; i < len(configfile); i++ {
		if strings.Contains(configfile[i], `"`) {
			configfile[i] = strings.ReplaceAll(configfile[i], `"`, "")
		}
		if strings.Contains(configfile[i], "export") {
			splitcmd := strings.SplitN(configfile[i], " ", 2)
			splittedvar := strings.SplitN(splitcmd[1], "=", 2)
			os.Setenv(splittedvar[0], splittedvar[1])
		}
	}
}

func main() {
	fmt.Println("buildintegra")
	fmt.Println(strings.Join(os.Args, ""))
	// start from 2, 2 for pkgname, 3 for pkgver, 4 for intgroot, 5 for pkgdir
	if strings.Contains(strings.Join(os.Args, ""), "PackageWithFakeroot") {
		fakeroot = true
		fakerootToPackage = os.Args[2]
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

	textf := string(file)
	textFile := strings.Split(textf, "\n")
	status := "setup"
	frSkipFunc := false
	shiftpack := int(0)
	for i := 0; i < len(textFile); i++ {
		textFile[i] = strings.TrimSpace(textFile[i])
		if frSkipFunc && strings.HasPrefix(textFile[i], ":end") {
			frSkipFunc = false
			continue
		} else if frSkipFunc {
			continue
		}

		if strings.HasPrefix(textFile[i], "//") || textFile[i] == "" {
			continue
		}

		if strings.Contains(textFile[i], "$") {
			textFile[i] = strings.ReplaceAll(textFile[i], "$pkgdir", pkgdir)
			textFile[i] = strings.ReplaceAll(textFile[i], "$srcdir", srcdir)
			textFile[i] = strings.ReplaceAll(textFile[i], "$intgroot", intgrootdir)
			textFile[i] = strings.ReplaceAll(textFile[i], "$pkgver", version)
		}

		if strings.HasSuffix(textFile[i], `\`) {
			ii := 0
			for ii = 0; len(textFile[i:]) > ii; ii++ {
				if !strings.HasSuffix(textFile[i+ii], `\`) {
					break
				}
			}
			textFile = append(textFile[:i+1], textFile[i+ii+1:]...)
		}
		if strings.Contains(textFile[i], " = ") && !strings.Contains(textFile[i], "export") && (status == "setup" || fakeroot) {
			maybevar := strings.Split(textFile[i], " = ")
			switch maybevar[0] {
			case "packagename":
				packagename = append(packagename, maybevar[1])
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
			case "url":
				url = maybevar[1]
			case "source":
				source = append(source, maybevar[1])
			default:
				//not var!
			}
			continue
		}

		if strings.Contains(textFile[i], "build:") {
			if fakeroot {
				frSkipFunc = true
				continue
			}
			status = "build"
			fmt.Println(intb, "Start build...")
			os.Chdir(srcdir)
			// prep source
			for _, v := range source {
				if strings.Contains(v, ".git") {
					executecmd("git", "clone", v)
				} else {
					executecmd(downloader, downloaderopt, v)
				}
			}
			continue
		} else if strings.HasPrefix(textFile[i], "package") {
			if len(packagename) == 1 {
				if !fakeroot {
					fmt.Println(intb, "Start packaging...")
				}
				os.RemoveAll(pkgdir)
				os.Chdir(intgrootdir)
				status = "package"
				os.Mkdir("package", os.ModePerm)
				if !fakeroot {
					fmt.Println(intb, "Start fakeroot environment...")
					executecmd("fakeroot", os.Args[0], "PackageWithFakeroot", packagename[0])
					frSkipFunc = true
				}
				continue
			} else {
				toSplit := []rune(textFile[i])
				toSplit = toSplit[:len(toSplit)-1]
				subpackagename := strings.Split(string(toSplit), " ")[1]
				subpackageavaliable := false
				for i := 0; len(packagename) > i; i++ {
					if subpackagename == packagename[i] {
						subpackageavaliable = true
					}
				}
				if !subpackageavaliable {
					continue
				}
				if fakeroot && fakerootToPackage != subpackagename {
					frSkipFunc = true
					continue
				}
				if !fakeroot {
					fmt.Println(intb, "Start packaging ", subpackagename, " ...")
				}
				os.RemoveAll(pkgdir)
				os.Chdir(intgrootdir)
				os.Mkdir("package", os.ModePerm)
				status = "package"
				if !fakeroot {
					fmt.Println(intb, "Start fakeroot environment...")
					executecmd("fakeroot", os.Args[0], "PackageWithFakeroot", subpackagename)
					frSkipFunc = true
				}
				continue
			}
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
				if !fakeroot {
					continue
				}
				if len(packagename) == 1 {
					os.WriteFile(pkgdir+"/.PACKAGE", []byte(generatePackInfo(packagename[0])), 0644)
					startpack(intgrootdir, packagename[0])
					status = "packfin"
					fmt.Println(intb, "Package Finished!!")
					os.Exit(0)
				} else {
					os.WriteFile(pkgdir+"/.PACKAGE", []byte(generatePackInfo(packagename[shiftpack])), 0644)
					startpack(intgrootdir, fakerootToPackage)
				}
			}
		} else if status == "build" || status == "package" {
			splitcmd := splitNparse(textFile[i])
			err := executecmdwitherror(splitcmd[0], splitcmd[1:]...)
			if err != nil {
				log.Fatal(err)
				continue
			}
		}
		if status == "packfin" && len(packagename) == 1 {
			os.Exit(0)
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

func startpack(intgroot string, packagename string) {
	os.Chdir(pkgdir)
	archivename := intgroot + "/" + packagename + "-" + version + ".intg.tar.zst"
	executecmd("bsdtar", "-cf", intgroot+"/"+packagename+"-"+version+".intg.tar.zst", ".",
		"--exclude", "MTREE", "--exclude", ".PACKAGE",
	)
	archivefile, err := os.Open(archivename)
	if err != nil {
		log.Fatal("error during reading archive file")
	}
	defer archivefile.Close()

	archivereader := bufio.NewReader(archivefile)

	fmt.Println(intb, "Generating MTREE File with bsdtar...")
	executecmdwithstdinfile(archivereader, "bsdtar", "-cf", ".MTREE",
		"--format=mtree", "--options", "!all,use-set,type,uid,gid,mode,time,size,sha256,link",
		"@-", "--exclude", "MTREE", "--exclude", ".PACKAGE",
	)

	fmt.Println(intb, "Creating main archive with bsdtar...")
	executecmd("bsdtar", "-cf", intgroot+"/"+packagename+"-"+version+".intg.tar.zst", ".",
		"--exclude", ".MTREE", "--exclude", ".PACKAGE")

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

func generatePackInfo(packagename string) (reText string) {
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

func splitNparse(cmdIn string) (returnSlice []string) {
	cmdrune := []rune(cmdIn)
	returnSlice = append(returnSlice, "")
	currentSlice := 0
	for prevChar := int(0); len(cmdrune) > prevChar; prevChar++ {
		if string(cmdrune[prevChar]) == `"` {
			i := strings.Index(string(cmdrune[prevChar+1:]), `"`)
			prevChar += 1
			returnSlice[currentSlice] += string(cmdrune[prevChar : prevChar+i])
			prevChar += i
		} else if string(cmdrune[prevChar]) == ` ` {
			currentSlice++
			returnSlice = append(returnSlice, "")
		} else {
			returnSlice[currentSlice] += string(cmdrune[prevChar])
		}
	}
	return returnSlice
}
