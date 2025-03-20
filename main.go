// see LICENSE file for the license
// simple package builder but not turing-complete

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
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

	downloader     = "aria2c"
	downloaderopts = []string{"--auto-file-renaming=false"}

	pkgdir string
	srcdir string

	fakeroot          = false
	fakerootToPackage = ""
)

// build(integra) options
var (
	lto = true
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

		if strings.HasSuffix(configfile[i], `\`) {
			ii := 0
			toappend := string("")
			for ii = 0; len(configfile[i:]) > ii; ii++ {
				if !strings.HasSuffix(configfile[i+ii], `\`) {
					toappend += configfile[i+ii]
					break
				} else {
					runeline := []rune(configfile[i+ii])
					toappend += string(runeline[:len(runeline)-1])
				}
			}
			configfile = append(append(configfile[:i], toappend), configfile[i+ii+1:]...)
		}

		if strings.Contains(configfile[i], "export") {
			_, spcmd, _ := strings.Cut(configfile[i], " ")
			os.Setenv(envSetter(spcmd))
		} else if strings.Contains(configfile[i], "setopt") {
			_, splitcmd, chk := strings.Cut(configfile[i], " ")
			if !chk {
				fmt.Println("error found in config file at line ", i)
			}
			spcmd, sparg, chk := strings.Cut(splitcmd, "=")
			if !chk {
				fmt.Println("error found in config file at line ", i)
			}
			switch spcmd {
			case "downloader":
				downloader = sparg
			case "downloaderopt":
				downloaderopts = strings.Split(sparg, " ")
			}
		}
	}
}

func main() {
	fmt.Println("buildintegra")
	fmt.Println(strings.Join(os.Args, ""))
	// start from 2, 2 for pkgname, 3 for pkgver, 4 for intgroot, 5 for pkgdir
	if slices.Contains(os.Args, "PackageWithFakeroot") {
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
	os.Setenv("srcdir", filepath.Join(intgrootdir, "source"))
	srcdir = filepath.Join(intgrootdir, "source")
	os.Setenv("pkgdir", filepath.Join(intgrootdir, "package"))
	pkgdir = filepath.Join(intgrootdir, "package")

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

		// don't place custom commands before this code,
		// it proceeds comments and empty lines.
		if strings.HasPrefix(textFile[i], "//") || textFile[i] == "" {
			continue
		}

		// and here, it proceeds to continue line
		if strings.HasSuffix(textFile[i], `\`) {
			ii := 0
			toappend := string("")
			for ii = 0; len(textFile[i:]) > ii; ii++ {
				if !strings.HasSuffix(textFile[i+ii], `\`) {
					toappend += textFile[i+ii]
					break
				} else {
					runeline := []rune(textFile[i+ii])
					toappend += string(runeline[:len(runeline)-1])
				}
			}
			textFile = append(append(textFile[:i], toappend), textFile[i+ii+1:]...)
		}

		if strings.Contains(textFile[i], "$pkgname") && status != "package" {
			fmt.Println(intb, "err: you can't use pkgname with outside of package (currently)")
		}

		if strings.Contains(textFile[i], "${") {
			// it just works
			// replace variable while contains
			for strings.Contains(textFile[i], "${") {
				first := strings.Index(textFile[i], "${")
				last := strings.Index(textFile[i], "}")
				uncutt := textFile[i][first : last+1]
				cutted := textFile[i][first+2 : last]
				textFile[i] = strings.Replace(textFile[i], uncutt, os.Getenv(cutted), -1)
			}
		} else if strings.Contains(textFile[i], "$") {
			/*pwd, err := os.Getwd()
			if err != nil {
				log.Fatal("failed to get working directory(why)")
			}*/
			pkgname := func() (aa string) {
				if len(packagename) == 1 {
					return packagename[0]
				} else {
					return fakerootToPackage
				}
			}()
			varReplacer := strings.NewReplacer(
				"$pkgdir", pkgdir,
				"$srcdir", srcdir,
				"$intgroot", intgrootdir,
				"$pkgname", pkgname,
				"$pkgver", version,
				"$pwd", rsUnwrap(os.Getwd()),
			)
			textFile[i] = varReplacer.Replace(textFile[i])
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
				conflicts = append(depends, maybevar[1])
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

		// safe to modify from here (maybe)
		{
			if strings.Contains(textFile[i], "options") {
				splOpt := strings.Split(textFile[i], " ")[0:]
				wg := &sync.WaitGroup{}
				for i := 0; i < len(splOpt); i++ {
					wg.Add(1)
					go func() {
						if splOpt[i] == "!lto" {
							lto = false
						} else if splOpt[i] == "lto" {
							lto = true
						}
						wg.Done()
					}()
					wg.Wait()
				}
			}
		}

		if strings.Contains(textFile[i], "build:") {
			if fakeroot {
				frSkipFunc = true
				continue
			}
			status = "build"
			fmt.Println(intb, "Start build...")
			ltoflags := os.Getenv("LTOFLAGS")
			if ltoflags != "" && lto {
				os.Setenv("CFLAGS", os.Getenv("CFLAGS")+" "+ltoflags)
				os.Setenv("CXXFLAGS", os.Getenv("CXXFLAGS")+" "+ltoflags)
				os.Setenv("LDFLAGS", os.Getenv("LDFLAGS")+" "+ltoflags)
			}
			os.Chdir(srcdir)
			// prep source
			for _, v := range source {
				if strings.HasSuffix(v, ".git") {
					executecmd("git", "clone", v)
				} else if strings.HasPrefix(v, "git") {
					executecmd("git", "clone", string([]rune(v)[4:]))
				} else {
					executecmd(downloader, append(downloaderopts, v)...)
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
				// reduce overwrite with splitted dir
				// also, cd to intgroot(where INTGBUILD files are available)
				// prepare for start fakeroot on correct directory
				os.Chdir(intgrootdir)
				status = "package"
				pdir := filepath.Join(intgrootdir, "pkg-"+subpackagename)
				os.Mkdir(pdir, os.ModePerm)
				pkgdir = pdir
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
			if strings.Contains(textFile[i], "export") {
				splitcmd := strings.SplitN(textFile[i], " ", 2)
				os.Setenv(envSetter(splitcmd[1]))
			}
		} else if strings.HasPrefix(textFile[i], ":end") {
			switch strings.Split(textFile[i], " ")[1] {
			case "build":
				status = "buildfin"
				fmt.Println(intb, "Build Finished.")
			case "package":
				if !fakeroot {
					continue
				}
				// single package
				if len(packagename) == 1 {
					os.WriteFile(filepath.Join(pkgdir, ".PACKAGE"), []byte(generatePackInfo(packagename[0])), 0644)
					startpack(intgrootdir, packagename[0], false)
					status = "packfin"
					fmt.Println(intb, "Package Finished!!")
					os.Exit(0)
				} else // multi-package
				{
					os.WriteFile(filepath.Join(pkgdir, ".PACKAGE"), []byte(generatePackInfo(packagename[shiftpack])), 0644)
					startpack(intgrootdir, fakerootToPackage, true)
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

func startpack(intgroot string, packagename string, dirpersubpkg bool) {
	if dirpersubpkg {
		pdir := filepath.Join(intgroot, "pkg-"+packagename)
		os.Mkdir(pdir, os.ModePerm)
		os.Chdir(pdir)
	} else {
		os.Mkdir(pkgdir, os.ModePerm)
		os.Chdir(pkgdir)
	}

	archivename := filepath.Join(intgroot, packagename+"-"+version+".intg.tar.zst")
	executecmd("bsdtar", "-cf", filepath.Join(intgroot, packagename+"-"+version+".intg.tar.zst"), ".",
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
	executecmd("bsdtar", "-cf", filepath.Join(intgroot, packagename+"-"+version+".intg.tar.zst"), ".",
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

func envSetter(inst string) (name string, env string) {
	if strings.Contains(inst, "+=") {
		splittedvar := strings.SplitN(inst, "+=", 2)
		return splittedvar[0], os.Getenv(splittedvar[0]) + " " + splittedvar[1]
	} else if strings.Contains(inst, "-=") {
		splittedvar := strings.SplitN(inst, "-=", 2)
		return splittedvar[0], strings.TrimSpace(strings.ReplaceAll(os.Getenv(splittedvar[0]), splittedvar[1], ""))
	} else if strings.Contains(inst, "=") {
		splittedvar := strings.SplitN(inst, "=", 2)
		return splittedvar[0], splittedvar[1]
	}
	return "", ""
}

func rsUnwrap[T any](val T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return val
}
