package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const DefaultBaseURL = "https://github.com/mattsp1290/lotel/releases"

var tagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Manifest struct {
	Schema  int               `json:"schema"`
	Version string            `json:"version"`
	Tag     string            `json:"tag"`
	Assets  map[string]string `json:"assets"`
	SHA256  map[string]string `json:"sha256"`
}

type Request struct {
	Current       string
	Executable    string
	BaseURL       string
	Version       string
	Check         bool
	Force         bool
	SourceBuild   bool
	BeforeReplace func(context.Context) error
}

type Report struct {
	Current            string `json:"current"`
	Latest             string `json:"latest"`
	Target             string `json:"target"`
	Path               string `json:"path"`
	UpdateAvailable    bool   `json:"update_available"`
	Upgraded           bool   `json:"upgraded"`
	CollectorRestarted bool   `json:"collector_restarted"`
}

type release struct {
	tag    string
	url    string
	digest string
}

func Run(ctx context.Context, req Request) (Report, error) {
	report := Report{Current: req.Current}
	if req.Version != "" && !tagPattern.MatchString(req.Version) {
		return report, fmt.Errorf("--version must look like v1.2.3")
	}
	base := strings.TrimRight(req.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	if err := validateBase(base); err != nil {
		return report, err
	}
	path, err := filepath.EvalSymlinks(req.Executable)
	if err != nil {
		return report, fmt.Errorf("resolve executable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return report, err
	}
	report.Path = path
	target, err := platformTarget()
	if err != nil {
		return report, err
	}
	manifestURL := base + "/latest/download/lotel-manifest.json"
	if req.Version != "" {
		manifestURL = base + "/download/" + req.Version + "/lotel-manifest.json"
	}
	var data strings.Builder
	manifestCtx, cancelManifest := context.WithTimeout(ctx, 20*time.Second)
	defer cancelManifest()
	if err := fetch(manifestCtx, manifestURL, 1<<20, &data); err != nil {
		return report, err
	}
	var manifest Manifest
	if err := json.Unmarshal([]byte(data.String()), &manifest); err != nil {
		return report, fmt.Errorf("decode release manifest: %w", err)
	}
	r, err := validateManifest(manifest, base, target, req.Version)
	if err != nil {
		return report, err
	}
	report.Latest, report.Target = r.tag, r.tag
	comparable := tagPattern.MatchString(req.Current)
	report.UpdateAvailable = !comparable || less(req.Current, r.tag)
	if req.Check {
		return report, nil
	}
	if req.SourceBuild && !req.Force {
		return report, fmt.Errorf("source build is protected; rebuild from source or use --force to replace it with a release")
	}
	if !comparable && !req.Force && req.Version == "" {
		return report, fmt.Errorf("current version %q is not a release; use --version or --force", req.Current)
	}
	if req.Version == "" && comparable && !report.UpdateAvailable {
		return report, nil
	}
	if req.Version != "" && req.Current == r.tag {
		return report, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".lotel-upgrade-*")
	if err != nil {
		return report, fmt.Errorf("stage upgrade: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	if err := fetch(ctx, r.url, 256<<20, io.MultiWriter(tmp, hash)); err != nil {
		return report, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != r.digest {
		return report, fmt.Errorf("release checksum mismatch")
	}
	if err := tmp.Chmod(0o755); err != nil {
		return report, err
	}
	if err := tmp.Sync(); err != nil {
		return report, err
	}
	if err := tmp.Close(); err != nil {
		return report, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(probeCtx, tmp.Name(), "--version")
	probe.WaitDelay = time.Second
	var output limitedOutput
	probe.Stdout, probe.Stderr = &output, io.Discard
	if err := probe.Run(); err != nil {
		return report, fmt.Errorf("downloaded executable failed version check: %w", err)
	}
	if strings.TrimSuffix(output.String(), "\n") != "lotel "+r.tag {
		return report, fmt.Errorf("downloaded executable reports an unexpected version")
	}
	if req.BeforeReplace != nil {
		if err := req.BeforeReplace(ctx); err != nil {
			return report, err
		}
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return report, fmt.Errorf("replace executable: %w", err)
	}
	report.Upgraded = true
	return report, nil
}

func platformTarget() (string, error) {
	osName := map[string]string{"linux": "linux", "darwin": "macos"}[runtime.GOOS]
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if osName == "" || arch == "" {
		return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return osName + "-" + arch, nil
}

func validateBase(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("invalid release base URL")
	}
	if u.Scheme == "https" && u.Host != "" {
		return nil
	}
	if u.Scheme == "file" && u.Host == "" && filepath.IsAbs(u.Path) {
		return nil
	}
	return fmt.Errorf("release base URL must use HTTPS or a local absolute file:// path")
}

func validateManifest(m Manifest, base, target, requested string) (release, error) {
	if m.Schema != 1 {
		return release{}, fmt.Errorf("unsupported release manifest schema")
	}
	if !tagPattern.MatchString(m.Tag) || m.Tag != "v"+m.Version {
		return release{}, fmt.Errorf("invalid manifest version or tag")
	}
	if requested != "" && requested != m.Tag {
		return release{}, fmt.Errorf("manifest does not match requested version")
	}
	expected := base + "/download/" + m.Tag + "/lotel-" + target
	if m.Assets[target] != expected {
		return release{}, fmt.Errorf("unexpected release asset URL for %s", target)
	}
	digest := m.SHA256[target]
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(digest) != digest {
		return release{}, fmt.Errorf("invalid release SHA-256 for %s", target)
	}
	return release{m.Tag, expected, digest}, nil
}

func less(a, b string) bool {
	x, y := strings.Split(a[1:], "."), strings.Split(b[1:], ".")
	for i := range x {
		if len(x[i]) != len(y[i]) {
			return len(x[i]) < len(y[i])
		}
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

func fetch(ctx context.Context, rawURL string, limit int64, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	var body io.ReadCloser
	if u.Scheme == "file" {
		if u.Host != "" {
			return fmt.Errorf("file URL must be local")
		}
		body, err = os.Open(u.Path)
	} else {
		client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("release redirect must use HTTPS")
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many release redirects")
			}
			return nil
		}}
		var request *http.Request
		request, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err == nil {
			var response *http.Response
			response, err = client.Do(request)
			if err == nil {
				if response.StatusCode != http.StatusOK {
					response.Body.Close()
					return fmt.Errorf("download %s: HTTP %d", rawURL, response.StatusCode)
				}
				body = response.Body
			}
		}
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer body.Close()
	n, err := io.Copy(out, io.LimitReader(body, limit+1))
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	if n > limit {
		return fmt.Errorf("release download exceeds %d bytes", limit)
	}
	return nil
}

type limitedOutput struct{ data []byte }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(w.data)+len(p) > 256 {
		return 0, fmt.Errorf("version output exceeds 256 bytes")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}
func (w *limitedOutput) String() string { return string(w.data) }
