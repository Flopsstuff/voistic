// Package release fetches voistic's releases from GitHub: the server for this
// computer and the firmware for the buttons, each checked against the release's
// SHA256SUMS.
package release

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is where the releases are.
const Repo = "Flopsstuff/voistic"

// API is GitHub's, a variable for the tests.
var API = "https://api.github.com"

const (
	sumsName    = "SHA256SUMS"
	maxDownload = 64 << 20
)

var client = &http.Client{Timeout: 2 * time.Minute}

// Release is one published version.
type Release struct {
	Tag    string
	assets map[string]string // name to download URL
	sums   map[string]string // name to hex SHA-256
}

// Latest asks GitHub for the newest release.
func Latest(ctx context.Context) (*Release, error) {
	var answer struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	data, err := get(ctx, API+"/repos/"+Repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("can't ask GitHub for the latest release: %w", err)
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, err
	}
	r := &Release{Tag: answer.Tag, assets: map[string]string{}}
	for _, asset := range answer.Assets {
		r.assets[asset.Name] = asset.URL
	}
	return r, nil
}

// Fetch downloads a file of the release and checks it against SHA256SUMS.
func (r *Release) Fetch(ctx context.Context, name string) ([]byte, error) {
	if r.sums == nil {
		url, ok := r.assets[sumsName]
		if !ok {
			return nil, fmt.Errorf("%s has no %s", r.Tag, sumsName)
		}
		data, err := get(ctx, url, "")
		if err != nil {
			return nil, err
		}
		r.sums = parseSums(data)
	}
	url, ok := r.assets[name]
	if !ok {
		return nil, fmt.Errorf("%s has no %s", r.Tag, name)
	}
	want, ok := r.sums[name]
	if !ok {
		return nil, fmt.Errorf("%s isn't in the %s of %s", name, sumsName, r.Tag)
	}
	data, err := get(ctx, url, "")
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("%s doesn't match its SHA-256", name)
	}
	return data, nil
}

func get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

// parseSums reads sha256sum's output, "hash  name" or "hash *name" a line.
func parseSums(data []byte) map[string]string {
	sums := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		hash, name, ok := strings.Cut(scanner.Text(), " ")
		if ok {
			sums[strings.TrimLeft(name, " *")] = strings.ToLower(hash)
		}
	}
	return sums
}

// ServerArchive names the archive of the server for a system, as make dist does.
func ServerArchive(goos, goarch string) string {
	name := "voistic-" + goos + "-" + goarch
	if goos == "windows" {
		return name + ".zip"
	}
	return name + ".tar.gz"
}

// Binaries takes the programs out of a server archive, by their base name.
func Binaries(archive []byte, zipped bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	if zipped {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if file.FileInfo().IsDir() {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(opened, maxDownload))
			opened.Close()
			if err != nil {
				return nil, err
			}
			files[path.Base(file.Name)] = data
		}
		return files, nil
	}
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxDownload))
		if err != nil {
			return nil, err
		}
		files[path.Base(header.Name)] = data
	}
}

// FirmwareName names the release's image for a board, as release.yml does. The
// XIAO has one with a microphone and one without.
func FirmwareName(board string, mic bool) (string, error) {
	switch {
	case board == "iot-button-c6":
		return "firmware-iot-button-c6.bin", nil
	case board == "xiao-s3" && mic:
		return "firmware-xiao-s3-mic.bin", nil
	case board == "xiao-s3":
		return "firmware-xiao-s3.bin", nil
	}
	return "", fmt.Errorf("no firmware in the releases for a %q board", board)
}

// FirmwareVersion finds the version a release's image reports, the tag and its
// build time, such as v0.2.1+1104100707, which scripts/version.py stamps into
// it. The server compares it with what the button says it runs.
func FirmwareVersion(image []byte, tag string) (string, error) {
	pattern := regexp.MustCompile(regexp.QuoteMeta(tag) + `\+[0-9]{10}`)
	found := pattern.Find(image)
	if found == nil {
		return "", fmt.Errorf("no %s version in the firmware", tag)
	}
	return string(found), nil
}

// Newer reports whether tag is a later version than current. A build between
// releases, such as v0.2.1-3-gabc-dirty, counts as its release.
func Newer(tag, current string) bool {
	latest, ok := parse(tag)
	if !ok {
		return false
	}
	running, ok := parse(current)
	if !ok {
		return true // dev
	}
	for i := range latest {
		if latest[i] != running[i] {
			return latest[i] > running[i]
		}
	}
	return false
}

func parse(version string) ([3]int, bool) {
	var parts [3]int
	version, ok := strings.CutPrefix(version, "v")
	if !ok {
		return parts, false
	}
	if end := strings.IndexAny(version, "-+"); end >= 0 {
		version = version[:end]
	}
	fields := strings.Split(version, ".")
	if len(fields) != 3 {
		return parts, false
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// Replace puts data in place of the program at executable. A running program
// can't be overwritten on Windows, but it can be renamed out of the way.
func Replace(executable string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(executable), ".voistic-new-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := executable + ".old"
		os.Remove(old)
		if err := os.Rename(executable, old); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tmp.Name(), executable)
}
