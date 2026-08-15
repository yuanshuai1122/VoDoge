package extensions

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxUncompressed = 256 << 20

func extractPluginZip(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%w: 不是 zip 包", ErrInvalidManifest)
	}
	prefix := detectZipRoot(zr)
	var total int64
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if prefix != "" {
			if name == prefix || !strings.HasPrefix(name, prefix+"/") {
				continue
			}
			name = strings.TrimPrefix(name, prefix+"/")
		}
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if !safeRelativePath(name) {
			return fmt.Errorf("%w: %s", ErrUnsafePath, f.Name)
		}
		if !f.FileInfo().Mode().IsRegular() {
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > maxUncompressed {
			return ErrTooLarge
		}
		if err := extractZipFile(f, filepath.Join(dest, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	return nil
}

func detectZipRoot(zr *zip.Reader) string {
	var top []string
	seen := map[string]struct{}{}
	hasRootManifest := false
	for _, f := range zr.File {
		name := strings.Trim(strings.ReplaceAll(f.Name, `\`, "/"), "/")
		if name == "" {
			continue
		}
		if name == ManifestFilename {
			hasRootManifest = true
		}
		first := strings.SplitN(name, "/", 2)[0]
		if _, ok := seen[first]; ok {
			continue
		}
		seen[first] = struct{}{}
		top = append(top, first)
	}
	if hasRootManifest || len(top) != 1 {
		return ""
	}
	return top[0]
}

func extractZipFile(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm()|0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(rc, maxUncompressed)); err != nil {
		return err
	}
	return nil
}

func readManifestFile(dir string) (Manifest, error) {
	for _, name := range []string{ManifestFilename} {
		path := filepath.Join(dir, name)
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Manifest{}, err
		}
		m, err := DecodeManifest(f)
		_ = f.Close()
		if err != nil {
			return Manifest{}, err
		}
		return m, nil
	}
	return Manifest{}, fmt.Errorf("%w: 缺少 %s", ErrInvalidManifest, ManifestFilename)
}
