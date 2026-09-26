package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultReleases = "https://github.com/kingswady/cw/releases"
	maxDownload     = 64 << 20 // an archive is a few MB; anything this large is wrong
)

func (a *app) update(args []string) error {
	fs := a.newFlags("update")
	check := fs.Bool("check", false, "only say whether a newer release exists")
	want := fs.String("version", "", "install this release (e.g. v0.3.0) instead of the latest")
	force := fs.Bool("force", false, "update even a development build, or reinstall the same version")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if version == "dev" && !*force {
		return usagef("this is a development build — use --force to replace it with a release")
	}
	target, err := a.executable()
	if err != nil {
		return fmt.Errorf("cannot find this program's file: %w", err)
	}
	if strings.Contains(target, "/Cellar/") {
		return fmt.Errorf("cw was installed with Homebrew — update it with: brew upgrade cw")
	}
	releases := strings.TrimRight(a.releases(), "/")
	tag := *want
	if tag == "" {
		if tag, err = latestTag(releases); err != nil {
			return err
		}
	}
	current := "v" + strings.TrimPrefix(version, "v")
	if !*force && !newer(tag, current) {
		fmt.Fprintf(a.stdout, "cw is up to date (%s).\n", current)
		return nil
	}
	if *check {
		fmt.Fprintf(a.stdout, "cw %s is available (this is %s) — run: cw update\n", tag, current)
		return nil
	}
	binary, err := fetchRelease(releases, tag)
	if err != nil {
		return err
	}
	if err := replaceExecutable(target, binary); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Updated cw %s → %s (%s)\n", current, tag, target)
	return nil
}

// downloadClient follows the redirects GitHub serves release files through,
// but only to https.
var downloadClient = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" && !isLoopback(req.URL.Hostname()) {
			return fmt.Errorf("refusing to follow a redirect to %s", req.URL)
		}
		if len(via) > 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// latestTag reads the tag from the releases/latest redirect — no API call, so
// no rate limit.
func latestTag(releases string) (string, error) {
	return latestTagWith(newHTTPClient(), releases)
}

func latestTagWith(httpClient *http.Client, releases string) (string, error) {
	resp, err := httpClient.Get(strings.TrimRight(releases, "/") + "/latest")
	if err != nil {
		return "", fmt.Errorf("cannot reach %s: %w", releases, err)
	}
	resp.Body.Close()
	location := resp.Header.Get("Location")
	tag := path.Base(location)
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("could not find the latest release at %s (HTTP %d)", releases, resp.StatusCode)
	}
	return tag, nil
}

func download(url string) ([]byte, error) {
	resp, err := downloadClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s (HTTP %d)", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err == nil && len(body) > maxDownload {
		err = fmt.Errorf("%s is larger than any cw release", url)
	}
	return body, err
}

// fetchRelease downloads this platform's archive of tag, checks it against
// the release's checksums.txt and returns the cw binary inside.
func fetchRelease(releases, tag string) ([]byte, error) {
	name := fmt.Sprintf("cw_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name = fmt.Sprintf("cw_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	}
	base := releases + "/download/" + tag + "/"
	archive, err := download(base + name)
	if err != nil {
		return nil, err
	}
	sums, err := download(base + "checksums.txt")
	if err != nil {
		return nil, err
	}
	expected := checksumFor(sums, name)
	if expected == "" {
		return nil, fmt.Errorf("%s is not in %s's checksums.txt", name, tag)
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expected {
		return nil, fmt.Errorf("checksum mismatch for %s — not updating", name)
	}
	if strings.HasSuffix(name, ".zip") {
		return fromZip(archive)
	}
	return fromTarGz(archive)
}

func checksumFor(sums []byte, name string) string {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name {
			return fields[0]
		}
	}
	return ""
}

func fromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("the archive holds no cw binary")
		}
		if err != nil {
			return nil, err
		}
		if header.Name == "cw" && header.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(reader, maxDownload))
		}
	}
}

func fromZip(archive []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, file := range reader.File {
		if file.Name == "cw.exe" {
			body, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer body.Close()
			return io.ReadAll(io.LimitReader(body, maxDownload))
		}
	}
	return nil, fmt.Errorf("the archive holds no cw.exe")
}

// replaceExecutable swaps target for binary atomically: written beside it,
// then renamed over it, so a failure never leaves half a program. Windows
// cannot overwrite a running .exe, so the old one is moved aside first.
func replaceExecutable(target string, binary []byte) error {
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, ".cw-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("cannot write to %s — rerun with sudo: sudo cw update", dir)
		}
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(binary); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp.Name(), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := target + ".old"
		_ = os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
	}
	return os.Rename(temp.Name(), target)
}

// newer reports whether release tag a is above b (vMAJOR.MINOR.PATCH).
func newer(a, b string) bool {
	pa, pb := semver(a), semver(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func semver(tag string) [3]int {
	var out [3]int
	parts := strings.SplitN(strings.TrimPrefix(tag, "v"), ".", 3)
	for i, part := range parts {
		digits := strings.TrimRightFunc(part, func(r rune) bool { return r < '0' || r > '9' })
		out[i], _ = strconv.Atoi(digits)
	}
	return out
}

// executablePath is this program's own file, symlinks resolved.
func executablePath() (string, error) {
	file, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(file)
}
