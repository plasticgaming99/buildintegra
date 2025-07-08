// integretity outputs integretity format
package integrity

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/zeebo/blake3"
)

var strb strings.Builder

func init() {
	strb.Grow(102400)
}

func Generate(rootpath string) string {
	filepath.WalkDir(rootpath, func(path string, d fs.DirEntry, err error) error {
		if d.Type().IsDir() {
			s, err := filepath.Rel(rootpath, path)
			if err != nil {
				log.Fatal("error processing path", err)
			}
			strb.WriteString("/")
			if s != "." {
				strb.WriteString(s)
			}
			strb.WriteString("\n")
		} else if d.Type().Perm().IsRegular() {
			fileByte, err := os.ReadFile(path)
			if err != nil {
				log.Fatal("error opening file")
			}
			inf, err := os.Stat(path)
			if err != nil {
				log.Fatal("error opening file")
			}
			stat := inf.Sys().(*syscall.Stat_t)
			sum := blake3.Sum256(fileByte)
			strb.WriteString(d.Name())
			strb.WriteString(" ")
			strb.WriteString("uid=")
			strb.WriteString(fmt.Sprint(stat.Uid))
			strb.WriteString(" ")
			strb.WriteString("gid=")
			strb.WriteString(fmt.Sprint(stat.Gid))
			strb.WriteString(" ")
			strb.WriteString("perm=")
			strb.WriteString(strconv.FormatUint(uint64(inf.Mode().Perm()), 8))
			strb.WriteString(" ")
			strb.WriteString("blake3sum=")
			strb.WriteString(hex.EncodeToString(sum[:]))
			strb.WriteString("\n")
		}
		return err
	})
	return strb.String()
}
