package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const (
	ManifestFilenameVodoge = "vodoge-plugin.json"
	ManifestFilenameVodog  = "vodog-plugin.json"
	ManifestFilenameVocat  = "vocat-plugin.json"
	SchemaVersion          = 1
)

func manifestFilenames() []string {
	return []string{ManifestFilenameVodoge, ManifestFilenameVodog, ManifestFilenameVocat}
}

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}[a-z0-9]$`)

var (
	ErrInvalidManifest = errors.New("plugin manifest invalid")
	ErrUnsafePath      = errors.New("plugin path is not a safe relative path")
)

type Contribution struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	LabelZH  string `json:"label_zh,omitempty"`
	LabelEN  string `json:"label_en,omitempty"`
	Location string `json:"location"`
	After    string `json:"after,omitempty"`
	Entry    string `json:"entry"`
}

type Backend struct {
	Commands map[string]string `json:"commands,omitempty"`
}

type Manifest struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Description   string         `json:"description,omitempty"`
	Author        string         `json:"author,omitempty"`
	Homepage      string         `json:"homepage,omitempty"`
	Permissions   []string       `json:"permissions,omitempty"`
	Contributions []Contribution `json:"contributions"`
	Backend       *Backend       `json:"backend,omitempty"`
}

func DecodeManifest(r io.Reader) (Manifest, error) {
	dec := json.NewDecoder(io.LimitReader(r, 256<<10))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("%w: 清单里只能有一个 JSON 对象", ErrInvalidManifest)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: 不支持的 schema_version %d", ErrInvalidManifest, m.SchemaVersion)
	}
	if !pluginIDPattern.MatchString(m.ID) {
		return fmt.Errorf("%w: id 须为 3–64 位小写字母、数字或连字符", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 100 {
		return fmt.Errorf("%w: name 必填且不超过 100 字", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Version) == "" || len(m.Version) > 64 {
		return fmt.Errorf("%w: version 必填且不超过 64 字", ErrInvalidManifest)
	}
	seen := make(map[string]struct{}, len(m.Contributions))
	for _, c := range m.Contributions {
		if !pluginIDPattern.MatchString(c.ID) {
			return fmt.Errorf("%w: 无效的 contribution id %q", ErrInvalidManifest, c.ID)
		}
		if _, dup := seen[c.ID]; dup {
			return fmt.Errorf("%w: 重复的 contribution id %q", ErrInvalidManifest, c.ID)
		}
		seen[c.ID] = struct{}{}
		if c.Location != "sidebar" && c.Location != "proxy" {
			return fmt.Errorf("%w: contribution %q 的 location 只能是 sidebar 或 proxy", ErrInvalidManifest, c.ID)
		}
		if strings.TrimSpace(c.Label) == "" {
			return fmt.Errorf("%w: contribution %q 需要 label", ErrInvalidManifest, c.ID)
		}
		if !safeRelativePath(c.Entry) {
			return fmt.Errorf("%w: contribution %q 的 entry 不安全", ErrInvalidManifest, c.ID)
		}
	}
	if m.Backend != nil {
		if len(m.Backend.Commands) == 0 {
			return fmt.Errorf("%w: backend.commands 为空", ErrInvalidManifest)
		}
		for platform, command := range m.Backend.Commands {
			if !strings.Contains(platform, "/") || !safeRelativePath(command) {
				return fmt.Errorf("%w: backend 命令 %q 无效", ErrInvalidManifest, platform)
			}
		}
	}
	perms := append([]string(nil), m.Permissions...)
	sort.Strings(perms)
	for i, p := range perms {
		if strings.TrimSpace(p) == "" || (i > 0 && p == perms[i-1]) {
			return fmt.Errorf("%w: permissions 必须非空且不重复", ErrInvalidManifest)
		}
	}
	return nil
}

func (m Manifest) BackendCommand() (string, bool) {
	if m.Backend == nil {
		return "", false
	}
	cmd, ok := m.Backend.Commands[runtime.GOOS+"/"+runtime.GOARCH]
	return cmd, ok
}

func safeRelativePath(value string) bool {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return false
	}
	for _, seg := range strings.Split(value, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
