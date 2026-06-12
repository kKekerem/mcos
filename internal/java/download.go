package java

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// adoptiumOSArch maps the running platform to Adoptium's os/arch identifiers.
func adoptiumOSArch() (osName, arch string) {
	switch runtime.GOOS {
	case "windows":
		osName = "windows"
	case "darwin":
		osName = "mac"
	default:
		osName = "linux"
	}
	switch runtime.GOARCH {
	case "arm64":
		arch = "aarch64"
	default:
		arch = "x64"
	}
	return
}

// temurinURL returns the Adoptium "latest GA" binary URL for a Java major.
func temurinURL(major int) string {
	osName, arch := adoptiumOSArch()
	return fmt.Sprintf(
		"https://api.adoptium.net/v3/binary/latest/%d/ga/%s/%s/jdk/hotspot/normal/eclipse?project=jdk",
		major, osName, arch,
	)
}

// downloadFile fetches url into a temp file and returns its path. The caller is
// responsible for removing the file.
func downloadFile(client *http.Client, url, tmpDir string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: unexpected status %s", resp.Status)
	}
	f, err := os.CreateTemp(tmpDir, "jdk-*.archive")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("download: copy: %w", err)
	}
	return f.Name(), nil
}

// extractArchive extracts a JDK archive (tar.gz on linux/mac, zip on windows)
// into dest, guarding against path traversal, and returns the JAVA_HOME inside
// dest (the directory containing bin/java[.exe]).
func extractArchive(archivePath, dest string) (string, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	var err error
	if runtime.GOOS == "windows" {
		err = extractZip(archivePath, dest)
	} else {
		err = extractTarGz(archivePath, dest)
	}
	if err != nil {
		return "", err
	}
	return findJavaHome(dest)
}

// safeJoin joins dest+name and ensures the result stays within dest (zip-slip).
func safeJoin(dest, name string) (string, error) {
	target := filepath.Join(dest, name)
	if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) && target != filepath.Clean(dest) {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	return target, nil
}

func extractTarGz(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFileFrom(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				// Non-fatal: some JDK archives include symlinks we can skip.
				continue
			}
		}
	}
}

func extractZip(archivePath, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		target, err := safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFileFrom(rc, target, zf.Mode())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFileFrom(r io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

// findJavaHome walks dest to locate the directory containing bin/java[.exe].
func findJavaHome(dest string) (string, error) {
	javaName := "java"
	if runtime.GOOS == "windows" {
		javaName = "java.exe"
	}
	var found string
	_ = filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() && d.Name() == javaName && filepath.Base(filepath.Dir(path)) == "bin" {
			found = filepath.Dir(filepath.Dir(path)) // strip /bin/java
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("java executable not found in extracted archive")
	}
	return found, nil
}

// defaultHTTPClient is a client with a generous timeout for large JDK downloads.
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}
