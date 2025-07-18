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

	"github.com/plasticgaming99/buildintegra/integrity"
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
	downloaderArgs = []string{"--auto-file-renaming=false"}

	gitExecutable = "git"
	gitArgs       = []string{}

	shell = "sh"

	pkgdir string
	srcdir string

	confFilePlace = "/etc/bintegra.conf"

	fakeroot          = false
	fakerootToPackage = ""

	additionalStats = []string{"prepare", "test"}
	cmdRunnableStat = []string{"prepare", "build", "package", "test"}
)

const intgBufSiz = 256

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

func main() {
	textFile := make([]string, 0, intgBufSiz)

	fmt.Println("buildintegra")
	fmt.Println(strings.Join(os.Args, ""))
	// start from 2, 2 for pkgname, 3 for pkgver, 4 for intgroot, 5 for pkgdir
	if slices.Contains(os.Args, "PackageWithFakeroot") {
		fakeroot = true
		fakerootToPackage = os.Args[2]
	}

	if cf := os.Getenv("BINTG_CONFIGFILE"); cf != "" {
		confFilePlace = cf
	}

	configFile, err := os.Open(confFilePlace)
	if err != nil {
		fmt.Println(intb, "Error reading configuration")
		return
	}
	bufConf := bufio.NewReader(configFile)
	confScanner := bufio.NewScanner(bufConf)
	for confScanner.Scan() {
		textFile = append(textFile, strings.TrimSpace(confScanner.Text()))
	}
	configFile.Close()

	intgFile, err := os.Open("INTGBUILD")
	if err != nil {
		fmt.Println("File isn't exists, or broken.")
		os.Exit(1)
	}
	bufIntg := bufio.NewReader(intgFile)
	intgScanner := bufio.NewScanner(bufIntg)
	for intgScanner.Scan() {
		textFile = append(textFile, strings.TrimSpace(intgScanner.Text()))
	}
	intgFile.Close()

	intgrootdir, err := os.Getwd()
	if err != nil {
		fmt.Println("internal error during getting root dir")
		os.Exit(1)
	}

	// yeah close them
	configFile.Close()
	intgFile.Close()

	initBuildDir()

	os.Setenv("intgroot", intgrootdir)
	os.Setenv("srcdir", filepath.Join(intgrootdir, "source"))
	srcdir = filepath.Join(intgrootdir, "source")
	os.Setenv("pkgdir", filepath.Join(intgrootdir, "package"))
	pkgdir = filepath.Join(intgrootdir, "package")

	status := "setup"
	frSkipFunc := false
	// maybe reusable cut
	var (
		key string
		val string
		con bool
	)
	for i := 0; i < len(textFile); i++ {
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

		// maybe I should add main pkg name like pkgbase
		if strings.Contains(textFile[i], "$pkgname") && status != "package" {
			fmt.Println(intb, "err: you should't use pkgname with outside of package block.")
			fmt.Println(strings.Repeat(" ", len([]rune(intb))), "first package name will be used.")
		}

		if strings.Contains(textFile[i], "${") {
			// it just works
			// replace variable while contains
			for strings.Contains(textFile[i], "${") {
				first := strings.Index(textFile[i], "${")
				last := strings.Index(textFile[i], "}")
				uncutt := textFile[i][first : last+1]
				cutted := textFile[i][first+2 : last]
				textFile[i] = strings.ReplaceAll(textFile[i], uncutt, os.Getenv(cutted))
			}
		}

		// replace var
		if strings.Contains(textFile[i], "$") {
			/*pwd, err := os.Getwd()
			if err != nil {
				log.Fatal("failed to get working directory(why)")
			}*/
			pkgname := func() (aa string) {
				if !fakeroot {
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
			key, val, con = strings.Cut(textFile[i], " = ")
			if !con {
				continue
			}
			switch key {
			case "packagename":
				packagename = append(packagename, val)
			case "version":
				version = val
			case "release":
				a, err := strconv.Atoi(val)
				if err != nil {
					fmt.Println("release number is not int")
				}
				release = a
			case "license":
				license = val
			case "architecture":
				architecture = val
			case "description":
				description = val
			case "depends":
				depends = append(depends, val)
			case "optdeps":
				optdeps = append(optdeps, val)
			case "builddeps":
				builddeps = append(builddeps, val)
			case "conflicts":
				conflicts = append(depends, val)
			case "provides":
				provides = append(provides, val)
			case "url":
				url = val
			case "source":
				source = append(source, val)
			default:
				//not var!
			}
			continue
		}

		// parsing is almost finished,
		// safe to modify from here (maybe)
		{
			if strings.Contains(textFile[i], "options") {
				splOpt := strings.Split(textFile[i], " ")
				for _, st := range splOpt {
					switch st {
					case "lto":
						lto = true
					case "!lto":
						lto = false
					default:
						continue
					}
				}
			}
		}

		if textFile[i] == "build:" {
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
					repo := v
					gitClone(repo)
				} else if strings.HasPrefix(v, "git") {
					repo := string([]rune(v)[4:])
					gitClone(repo)

				} else {
					executecmd(downloader, append(downloaderArgs, v)...)
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
				_, val, _ = strings.Cut(string(toSplit), " ")
				subpackagename := val
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
		} else
		// other status (not build, package)
		if slices.Contains(additionalStats, cutSingleRight(textFile[i])) {
			// nothing!!
			continue
		}

		// process internal command
		if strings.HasPrefix(textFile[i], "cd") {
			_, val, con = strings.Cut(textFile[i], " ")
			if con {
				os.Chdir(val)
			}
		} else if strings.HasPrefix(textFile[i], "export") {
			_, val, con = strings.Cut(textFile[i], " ")
			if con {
				os.Setenv(envSetter(val))
			}
		} else if strings.HasPrefix(textFile[i], "setopt") {
			_, val, con = strings.Cut(textFile[i], " ")
			if con {
				key, val, _ = strings.Cut(val, "=")
				switch key {
				case "downloader":
					downloader = val
				case "downloaderArgs":
					downloaderArgs = strings.Split(val, " ")
				case "git":
					gitExecutable = val
				case "gitArgs":
					gitArgs = strings.Split(val, " ")
				default:
					fmt.Println(intb, "Unknown option: ", val)
				}
			}
		} else if strings.HasPrefix(textFile[i], ":end") {
			_, val, con = strings.Cut(textFile[i], " ")
			if !con {
				continue
			}
			switch val {
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
					os.WriteFile(filepath.Join(pkgdir, ".PACKAGE"), []byte(generatePackInfo(fakerootToPackage)), 0644)
					startpack(intgrootdir, fakerootToPackage, true)
				}
			default:
				if slices.Contains(additionalStats, val) {
					fmt.Println(intb, "Step ", additionalStats, " finished.")
				}
			}
		} else

		// execute external command
		if slices.Contains(cmdRunnableStat, status) {
			var (
				splitcmd = splitNparse(textFile[i])
				maincmd  = 0
				osenv    = os.Environ()
				err      error
			)

			for i, s := range splitcmd {
				if strings.Contains(s, "=") {
					key, _, _ := strings.Cut(s, "=")
					osenv = slices.DeleteFunc(osenv, func(str string) bool {
						return strings.Contains(str, key+"=")
					})
					osenv = slices.Insert(osenv, 0, s)
				} else if s == "$" {
					continue
				} else {
					maincmd = i
					break
				}
			}

			if splitcmd[0] == "$" {
				shellarg := []string{"-c"}
				err = executeCmdEnvErr(shell, append(shellarg, strings.Join(splitcmd[maincmd:], " ")), osenv)
			} else {
				if len(splitcmd[maincmd:]) == maincmd {
					executeCmdEnvErr(splitcmd[maincmd], nil, osenv)
				}
				err = executeCmdEnvErr(splitcmd[maincmd], splitcmd[maincmd+1:], osenv)
			}
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

func cutSingleLeft(s string) string {
	return string([]rune(s)[0:])
}

func cutSingleRight(s string) string {
	return string([]rune(s)[:len([]rune(s))-1])
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

/*func executecmdwitherror(cmdname string, args ...string) error {
	toexec := exec.Command(cmdname, args...)
	toexec.Stdin = os.Stdin
	toexec.Stdout = os.Stdout
	toexec.Stderr = os.Stderr
	toexec.Env = os.Environ()
	return toexec.Run()
}*/

func executeCmdEnvErr(cmdname string, argv []string, envir []string) error {
	texec := &exec.Cmd{}
	if argv != nil {
		texec = exec.Command(cmdname, argv...)
	} else {
		texec = exec.Command(cmdname)
	}
	texec.Stdin = os.Stdin
	texec.Stdout = os.Stdout
	texec.Stderr = os.Stderr
	texec.Env = envir
	return texec.Run()
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

	/*archivename := filepath.Join(intgroot, packagename+"-"+version+".intg.tar.zst")
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
	)*/

	fmt.Println(integrity.Generate(pkgdir))
	fmt.Println(intb, "Generating .INTEGRITY...")
	os.WriteFile(pkgdir+"/.INTEGRITY", []byte(integrity.Generate(pkgdir)), 0644)

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

func gitClone(repo string) {
	repoName := strings.Split(repo, "/")
	repoDir, err := os.Stat(repoName[len(repoName)-1])
	if err != nil {
		if len(gitArgs) == 0 {
			executecmd(gitExecutable, "clone", repo)
		} else {
			gitOpts := append([]string{"clone"}, repo)
			gitOpts = append(gitOpts, gitArgs...)
			executecmd(gitExecutable, gitOpts...)
		}
	} else if repoDir.Mode().IsDir() {
		os.Chdir(repoDir.Name())
		executecmd(gitExecutable, "pull")
		os.Chdir("..")
	}
}
