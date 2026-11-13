// lumen-verify independently verifies a Lumen case export (.zip).
//
//	lumen-verify LUM-2026-0001_export_20261113T120000Z.zip
//
// It checks every file against SHA256SUMS, recomputes the hash of every
// custody event, and checks the prev-hash links between consecutive events.
// It needs no access to the Lumen server.
package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lumenforensics/lumen/internal/custody"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: lumen-verify <export.zip>")
		os.Exit(2)
	}
	ok, err := verify(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	if !ok {
		fmt.Println("\nRESULT: FAILED. This export does not match its recorded hashes.")
		os.Exit(1)
	}
	fmt.Println("\nRESULT: OK. All files and custody events verified.")
}

func verify(path string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	root := ""
	for _, f := range zr.File {
		files[f.Name] = f
		if strings.HasSuffix(f.Name, "/SHA256SUMS") {
			root = strings.TrimSuffix(f.Name, "SHA256SUMS")
		}
	}
	sums, ok := files[root+"SHA256SUMS"]
	if !ok {
		return false, fmt.Errorf("SHA256SUMS not found; is this a Lumen export?")
	}
	allOK := true

	fmt.Println("Files")
	rc, _ := sums.Open()
	sc := bufio.NewScanner(rc)
	n := 0
	for sc.Scan() {
		want, name, found := strings.Cut(sc.Text(), "  ")
		if !found {
			continue
		}
		n++
		f, ok := files[root+name]
		if !ok {
			fmt.Printf("  MISSING   %s\n", name)
			allOK = false
			continue
		}
		got, err := hashFile(f)
		if err != nil || got != want {
			fmt.Printf("  MISMATCH  %s\n", name)
			allOK = false
			continue
		}
		fmt.Printf("  OK        %s\n", name)
	}
	rc.Close()
	fmt.Printf("  %d files checked\n", n)

	fmt.Println("\nCustody log")
	logf, ok := files[root+"custody-log.jsonl"]
	if !ok {
		fmt.Println("  MISSING   custody-log.jsonl")
		return false, nil
	}
	lr, _ := logf.Open()
	defer lr.Close()
	sc = bufio.NewScanner(lr)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var prev *custody.Event
	count, links := 0, 0
	for sc.Scan() {
		var e custody.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return false, fmt.Errorf("custody log line %d: %w", count+1, err)
		}
		count++
		if !custody.VerifySelf(e) {
			fmt.Printf("  TAMPERED  event #%d (%s): content does not match its hash\n", e.Seq, e.Action)
			allOK = false
		}
		// The export holds this case's events only; check links where neighbours are present.
		if prev != nil && e.Seq == prev.Seq+1 {
			links++
			if e.PrevHash != prev.Hash {
				fmt.Printf("  BROKEN    link between #%d and #%d\n", prev.Seq, e.Seq)
				allOK = false
			}
		}
		prev = &e
	}
	fmt.Printf("  %d events self-verified, %d consecutive links checked\n", count, links)
	if prev != nil {
		fmt.Printf("  last event #%d hash %s\n", prev.Seq, prev.Hash)
	}
	return allOK, sc.Err()
}

func hashFile(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
