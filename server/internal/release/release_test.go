package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func serve(t *testing.T, files map[string][]byte, sums string) {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+Repo+"/releases/latest" {
			fmt.Fprint(w, `{"tag_name":"v0.3.0","assets":[`)
			first := true
			for name := range files {
				if !first {
					fmt.Fprint(w, ",")
				}
				first = false
				fmt.Fprintf(w, `{"name":%q,"browser_download_url":%q}`, name, server.URL+"/dl/"+name)
			}
			fmt.Fprintf(w, `,{"name":"SHA256SUMS","browser_download_url":%q}]}`, server.URL+"/dl/SHA256SUMS")
			return
		}
		name := filepath.Base(r.URL.Path)
		if name == "SHA256SUMS" {
			fmt.Fprint(w, sums)
			return
		}
		w.Write(files[name])
	}))
	t.Cleanup(server.Close)
	previous := API
	API = server.URL
	t.Cleanup(func() { API = previous })
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestFetchChecksSum(t *testing.T) {
	good, bad := []byte("good image"), []byte("tampered")
	serve(t, map[string][]byte{"a.bin": good, "b.bin": bad},
		sum(good)+"  a.bin\n"+sum([]byte("original"))+" *b.bin\n")
	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v0.3.0" {
		t.Errorf("tag = %q", r.Tag)
	}
	if data, err := r.Fetch(context.Background(), "a.bin"); err != nil || string(data) != "good image" {
		t.Errorf("a.bin = %q, %v", data, err)
	}
	if _, err := r.Fetch(context.Background(), "b.bin"); err == nil {
		t.Error("a file that doesn't match its sum should be refused")
	}
	if _, err := r.Fetch(context.Background(), "c.bin"); err == nil {
		t.Error("a missing file should fail")
	}
}

func TestBinariesFromTar(t *testing.T) {
	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	writer := tar.NewWriter(zipped)
	writer.WriteHeader(&tar.Header{Name: "voistic-linux-amd64/", Typeflag: tar.TypeDir, Mode: 0o755})
	writer.WriteHeader(&tar.Header{Name: "voistic-linux-amd64/voistic", Typeflag: tar.TypeReg, Mode: 0o755, Size: 3})
	writer.Write([]byte("bin"))
	writer.Close()
	zipped.Close()

	files, err := Binaries(archive.Bytes(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || string(files["voistic"]) != "bin" {
		t.Errorf("files = %q", files)
	}
}

func TestFirmwareVersion(t *testing.T) {
	image := []byte("\xe9junk\x00v0.3.0\x00v0.3.0+1104100707\x00more")
	if got, err := FirmwareVersion(image, "v0.3.0"); err != nil || got != "v0.3.0+1104100707" {
		t.Errorf("version = %q, %v", got, err)
	}
	if _, err := FirmwareVersion(image, "v0.4.0"); err == nil {
		t.Error("another tag should not be found")
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		tag, current string
		want         bool
	}{
		{"v0.3.0", "v0.2.1", true},
		{"v0.2.1", "v0.2.1", false},
		{"v0.2.1", "v0.2.1-3-gabc-dirty", false},
		{"v0.10.0", "v0.9.9", true},
		{"v0.2.0", "v0.2.1", false},
		{"v1.0.0", "dev", true},
		{"junk", "v0.1.0", false},
	} {
		if got := Newer(c.tag, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.tag, c.current, got)
		}
	}
}

func TestReplace(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "voistic")
	os.WriteFile(executable, []byte("old"), 0o755)
	if err := Replace(executable, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(executable)
	if string(data) != "new" {
		t.Errorf("got %q", data)
	}
	// Windows has no modes, and keeps the running program aside as .old.
	want := 1
	if runtime.GOOS == "windows" {
		want = 2
	} else if info, _ := os.Stat(executable); info.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", info.Mode())
	}
	if entries, _ := os.ReadDir(filepath.Dir(executable)); len(entries) != want {
		t.Errorf("left behind %v", entries)
	}
}

func TestFirmwareName(t *testing.T) {
	if name, _ := FirmwareName("xiao-s3", true); name != "firmware-xiao-s3-mic.bin" {
		t.Errorf("name = %q", name)
	}
	if _, err := FirmwareName("unknown", false); err == nil {
		t.Error("an unknown board should fail")
	}
}
