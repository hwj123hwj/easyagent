// Package skillmarket implements a client for the EasyCode official skill
// store (https://skills.deepvlab.ai) plus local install/uninstall management.
//
// Browse/search/detail requests are pure reads against the remote REST API.
// Installation downloads the server-provided zip, verifies its SHA-256 digest
// (when the store supplies one), extracts it into the personal skills
// directory, and records a manifest so later uninstalls only remove
// marketplace-owned files — never hand-written skills that happen to share
// the directory.
//
// Server metadata is the single source of truth: names, versions, and
// digests supplied by the caller are ignored in favor of the store's detail
// response, mirroring the reference client's behavior.
package skillmarket

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the EasyCode official skill store.
const DefaultBaseURL = "https://skills.deepvlab.ai"

// DefaultTimeout bounds every marketplace HTTP request.
const DefaultTimeout = 15 * time.Second

// manifestName is written into each installed skill directory.
const manifestName = ".easyagent-market.json"

// Skill naming rule shared with the SKILL.md loader (lowercase, digits, hyphens).
var packageNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ─── API models ──────────────────────────────────────────────────────────────

// Section is a thematic group returned by the store.
type Section struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	NameI18n    *string `json:"name_i18n"`
	Description *string `json:"description_i18n"`
	OrderIndex  int     `json:"order_index"`
}

// PreviewThumbnail is a lightweight card asset; the full preview is a long screenshot.
type PreviewThumbnail struct {
	URL    string `json:"url"`
	IsLong bool   `json:"is_long"`
}

// ExampleFile is a generated artifact available from a detail page.
type ExampleFile struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type,omitempty"`
	Extension string `json:"extension,omitempty"`
}

// SkillItem is a skill as returned by browse and detail endpoints.
// JSON tags preserve the REST contract; decoding also accepts the official store's camelCase fields.
type SkillItem struct {
	ID                int                 `json:"id"`
	Name              string              `json:"name"`
	DisplayName       string              `json:"display_name"`
	Description       string              `json:"description"`
	RichDescription   *string             `json:"rich_description"`
	IconURL           *string             `json:"icon_url"`
	PreviewImages     []string            `json:"preview_images"`
	PreviewThumbnails []*PreviewThumbnail `json:"preview_thumbnails"`
	UsageExample      *string             `json:"usage_example"`
	ExampleFiles      []ExampleFile       `json:"example_files"`
	Category          string              `json:"category"`
	Tags              []string            `json:"tags"`
	Version           string              `json:"version"`
	InstallCount      int                 `json:"install_count"`
	Featured          bool                `json:"featured"`
	SortOrder         int                 `json:"sort_order"`
	TarballSize       int64               `json:"tarball_size"`
	SHA256            *string             `json:"sha256"`
	OSSKey            *string             `json:"oss_key"`
	SectionID         *int                `json:"section_id"`
	SectionName       *string             `json:"section_name"`
}

// decodeStoreJSON accepts both the official store's camelCase wire format and
// the snake_case fields used by EasyAgent REST responses and existing fixtures.
// Explicit snake_case fields take precedence if both forms are present.
func decodeStoreJSON(data []byte, out any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	aliases := map[string]string{
		"displayName": "display_name", "richDescription": "rich_description",
		"iconUrl": "icon_url", "previewImages": "preview_images",
		"previewThumbnails": "preview_thumbnails", "usageExample": "usage_example",
		"exampleFiles": "example_files", "installCount": "install_count",
		"sortOrder": "sort_order", "tarballSize": "tarball_size", "ossKey": "oss_key",
		"sectionId": "section_id", "sectionName": "section_name",
		"nameI18n": "name_i18n", "descriptionI18n": "description_i18n",
		"orderIndex": "order_index", "isLong": "is_long", "mimeType": "mime_type",
	}
	for camel, snake := range aliases {
		if _, exists := fields[snake]; !exists {
			if value, ok := fields[camel]; ok {
				fields[snake] = value
			}
		}
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(normalized, out)
}

func (s *SkillItem) UnmarshalJSON(data []byte) error {
	type plain SkillItem
	return decodeStoreJSON(data, (*plain)(s))
}
func (s *Section) UnmarshalJSON(data []byte) error {
	type plain Section
	return decodeStoreJSON(data, (*plain)(s))
}
func (s *PreviewThumbnail) UnmarshalJSON(data []byte) error {
	type plain PreviewThumbnail
	return decodeStoreJSON(data, (*plain)(s))
}
func (s *ExampleFile) UnmarshalJSON(data []byte) error {
	type plain ExampleFile
	return decodeStoreJSON(data, (*plain)(s))
}

// BrowseQuery filters a store browse/search request.
type BrowseQuery struct {
	Category string
	Sort     string // "featured" | "installs" | "name"
	Query    string
	Page     int
	Locale   string
	Section  int
}

// BrowseResult is one page of store listings.
type BrowseResult struct {
	Skills []SkillItem `json:"skills"`
	Total  int         `json:"total"`
	Page   int         `json:"page"`
}

// InstallRecord is the on-disk manifest for one marketplace install.
type InstallRecord struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name,omitempty"`
	Version     string    `json:"version,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
	SourceURL   string    `json:"source_url,omitempty"`
}

// ─── Errors ──────────────────────────────────────────────────────────────────

var (
	// ErrNotFound is returned when the store has no skill for the given id.
	ErrNotFound = errors.New("skillmarket: skill not found")
	// ErrAlreadyInstalled means the target directory already holds a skill.
	ErrAlreadyInstalled = errors.New("skillmarket: install path already exists")
	// ErrNotMarketplace means the directory has no marketplace manifest.
	ErrNotMarketplace = errors.New("skillmarket: not a marketplace-installed skill")
	// ErrDigestMismatch means the downloaded zip failed SHA-256 verification.
	ErrDigestMismatch = errors.New("skillmarket: sha256 digest mismatch")
)

// ─── Client ──────────────────────────────────────────────────────────────────

// Client talks to the skill store REST API and manages local installs.
type Client struct {
	baseURL   string
	http      *http.Client
	skillsDir string // personal skills root, e.g. ~/.agents/skills

	// progress, when set, receives coarse install phase updates
	// (downloading with byte counts, extracting, verifying, done).
	mutation sync.Mutex
}

// InstallPhase identifies where an install currently is.
type InstallPhase string

const (
	PhaseResolving   InstallPhase = "resolving"   // fetching detail metadata
	PhaseDownloading InstallPhase = "downloading" // zip transfer, byte-level progress
	PhaseVerifying   InstallPhase = "verifying"   // sha256 / size checks
	PhaseExtracting  InstallPhase = "extracting"  // unpacking into skills dir
	PhaseDone        InstallPhase = "done"
)

// InstallProgress is one progress sample during InstallWithProgress.
type InstallProgress struct {
	Phase       InstallPhase `json:"phase"`
	SkillID     int          `json:"skill_id"`
	Name        string       `json:"name,omitempty"`
	DisplayName string       `json:"display_name,omitempty"`
	Version     string       `json:"version,omitempty"`
	// Download byte counters (PhaseDownloading only).
	BytesDone  int64 `json:"bytes_done,omitempty"`
	BytesTotal int64 `json:"bytes_total,omitempty"`
	// Err carries the failure message when the install aborted.
	Err string `json:"err,omitempty"`
}

// ProgressFunc receives progress samples; called synchronously from the
// installing goroutine, so implementers must not block.
type ProgressFunc func(InstallProgress)

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the store endpoint.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(u, "/")
	}
}

// WithHTTPClient overrides the underlying HTTP client (tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// NewClient creates a marketplace client installing into skillsDir.
func NewClient(skillsDir string, opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		http:      &http.Client{Timeout: DefaultTimeout},
		skillsDir: skillsDir,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL returns the configured store endpoint.
func (c *Client) BaseURL() string { return c.baseURL }

// SkillsDir returns the local install root.
func (c *Client) SkillsDir() string { return c.skillsDir }

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("skillmarket: API error %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Search browses the store. Any zero-value query field is omitted.
func (c *Client) Search(ctx context.Context, q BrowseQuery) (BrowseResult, error) {
	v := url.Values{}
	if q.Category != "" {
		v.Set("category", q.Category)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	if q.Query != "" {
		v.Set("q", q.Query)
	}
	if q.Page > 0 {
		v.Set("page", fmt.Sprint(q.Page))
	}
	if q.Locale != "" {
		v.Set("locale", q.Locale)
	}
	if q.Section > 0 {
		v.Set("section", fmt.Sprint(q.Section))
	}
	var raw struct {
		Skills []SkillItem `json:"skills"`
		Total  int         `json:"total"`
		Page   int         `json:"page"`
	}
	if err := c.get(ctx, "/api/skills", v, &raw); err != nil {
		return BrowseResult{}, err
	}
	if raw.Skills == nil {
		raw.Skills = []SkillItem{}
	}
	return BrowseResult{Skills: raw.Skills, Total: raw.Total, Page: raw.Page}, nil
}

// Detail fetches one skill's full metadata.
func (c *Client) Detail(ctx context.Context, id int, locale string) (SkillItem, error) {
	v := url.Values{}
	if locale != "" {
		v.Set("locale", locale)
	}
	var item SkillItem
	if err := c.get(ctx, fmt.Sprintf("/api/skills/%d", id), v, &item); err != nil {
		return SkillItem{}, err
	}
	return item, nil
}

// Sections lists the store's thematic groups.
func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var raw struct {
		Sections []Section `json:"sections"`
	}
	if err := c.get(ctx, "/api/sections", nil, &raw); err != nil {
		return nil, err
	}
	if raw.Sections == nil {
		raw.Sections = []Section{}
	}
	return raw.Sections, nil
}

// ─── Install / uninstall ─────────────────────────────────────────────────────

// Install downloads, verifies, and extracts the skill package into the
// personal skills directory without progress reporting.
func (c *Client) Install(ctx context.Context, id int) (SkillItem, error) {
	return c.InstallWithProgress(ctx, id, nil)
}

// InstallWithProgress is Install with phase/byte-level progress callbacks.
// Only server-provided metadata is trusted: the detail response supplies
// the name, version, and expected digest.
func (c *Client) InstallWithProgress(ctx context.Context, id int, progress ProgressFunc) (SkillItem, error) {
	c.mutation.Lock()
	defer c.mutation.Unlock()
	emit := func(p InstallProgress) {
		if progress != nil {
			progress(p)
		}
	}
	prog := func(phase InstallPhase, item SkillItem, err error) {
		p := InstallProgress{Phase: phase, SkillID: item.ID, Name: item.Name, DisplayName: item.DisplayName, Version: item.Version}
		if err != nil {
			p.Err = err.Error()
		}
		emit(p)
	}

	prog(PhaseResolving, SkillItem{ID: id}, nil)
	// Official metadata only — never trust agent-provided name/version/sha.
	item, err := c.Detail(ctx, id, "")
	if err != nil {
		prog(PhaseResolving, SkillItem{ID: id}, err)
		return SkillItem{}, err
	}
	if !packageNameRe.MatchString(item.Name) {
		err := fmt.Errorf("skillmarket: invalid package name %q", item.Name)
		prog(PhaseResolving, item, err)
		return SkillItem{}, err
	}

	target := filepath.Join(c.skillsDir, item.Name)
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		// Refuse to merge into (or later rm -rf) a user-owned directory.
		err := fmt.Errorf("%w: %s", ErrAlreadyInstalled, target)
		prog(PhaseDownloading, item, err)
		return SkillItem{}, err
	}
	prog(PhaseDownloading, item, nil)

	zipPath, err := c.downloadZipWithProgress(ctx, item, func(done, total int64) {
		emit(InstallProgress{Phase: PhaseDownloading, SkillID: item.ID, Name: item.Name, DisplayName: item.DisplayName, Version: item.Version, BytesDone: done, BytesTotal: total})
	})
	if err != nil {
		prog(PhaseDownloading, item, err)
		return SkillItem{}, err
	}
	defer os.Remove(zipPath)

	prog(PhaseVerifying, item, nil)
	prog(PhaseExtracting, item, nil)
	if err := os.MkdirAll(c.skillsDir, 0o755); err != nil {
		return SkillItem{}, err
	}
	stage, err := os.MkdirTemp(c.skillsDir, ".market-install-")
	if err != nil {
		return SkillItem{}, err
	}
	defer os.RemoveAll(stage)
	if err := extractZipContext(ctx, zipPath, stage); err != nil {
		prog(PhaseExtracting, item, err)
		return SkillItem{}, err
	}

	record := InstallRecord{
		ID:          item.ID,
		Name:        item.Name,
		DisplayName: item.DisplayName,
		Version:     item.Version,
		InstalledAt: time.Now().UTC(),
		SourceURL:   c.baseURL,
	}
	if item.SHA256 != nil {
		record.SHA256 = *item.SHA256
	}
	if err := writeManifest(stage, record); err != nil {
		prog(PhaseExtracting, item, err)
		return SkillItem{}, err
	}
	if err := ctx.Err(); err != nil {
		return SkillItem{}, err
	}
	// Publish the fully validated directory without replacing any target,
	// including a directory created by another process during the download.
	if err := renameNew(stage, target); err != nil {
		if os.IsExist(err) {
			return SkillItem{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, target)
		}
		return SkillItem{}, err
	}
	prog(PhaseDone, item, nil)
	return item, nil
}

// downloadZip fetches the package to a temp file, verifying the digest when
// the store provides one. Legacy rows without sha256 still install.
func (c *Client) downloadZip(ctx context.Context, item SkillItem) (string, error) {
	return c.downloadZipWithProgress(ctx, item, nil)
}

// progressStride throttles byte-level progress callbacks to one per 64 KiB.
const progressStride = 64 << 10

type countingWriter struct {
	pending int64
	onWrite func(int64)
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.pending += int64(len(p))
	if w.pending >= progressStride {
		w.onWrite(w.pending)
		w.pending = 0
	}
	return len(p), nil
}

// downloadZipWithProgress is downloadZip with byte-count callbacks (throttled
// to one per progressStride). BytesTotal may be 0 without a server length.
func (c *Client) downloadZipWithProgress(ctx context.Context, item SkillItem, onBytes func(done, total int64)) (string, error) {
	u := fmt.Sprintf("%s/api/skills/%d/download", c.baseURL, item.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("skillmarket: download failed: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "easyagent-skill-*.zip")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	total := item.TarballSize
	if total <= 0 {
		total = resp.ContentLength
	}
	hash := sha256.New()
	var counted int64
	dst := io.Writer(io.MultiWriter(tmp, hash))
	if onBytes != nil {
		dst = io.MultiWriter(tmp, hash, &countingWriter{onWrite: func(n int64) {
			counted += n
			onBytes(counted, total)
		}})
	}
	size, err := io.Copy(dst, io.LimitReader(resp.Body, maxPackageSize+1))
	if err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("skillmarket: download failed: %w", err)
	}
	if size > maxPackageSize {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("skillmarket: package exceeds size limit")
	}
	if onBytes != nil {
		// Flush the final count (stride throttling skips small files).
		onBytes(size, total)
	}
	if item.TarballSize > 0 && size != item.TarballSize {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("skillmarket: download size mismatch: got %d, want %d", size, item.TarballSize)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if item.SHA256 != nil {
		expected := strings.ToLower(strings.TrimSpace(*item.SHA256))
		if expected != "" && digest != expected {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, digest, expected)
		}
	}
	return tmp.Name(), nil
}

// maxPackageSize caps the downloaded zip at 200 MiB as a runaway-response guard.
const maxPackageSize = 200 << 20

// extractZip unpacks src into dst, rejecting path traversal ("zip slip")
// and absolute entry names. Missing parent directories are created.
func extractZip(src, dst string) error { return extractZipContext(context.Background(), src, dst) }

func extractZipContext(ctx context.Context, src, dst string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("skillmarket: invalid zip: %w", err)
	}
	defer reader.Close()

	if len(reader.File) > 10000 {
		return fmt.Errorf("skillmarket: too many zip entries")
	}
	var expanded uint64
	for _, f := range reader.File {
		if f.UncompressedSize64 > maxPackageSize || expanded > maxPackageSize-f.UncompressedSize64 {
			return fmt.Errorf("skillmarket: expanded package exceeds size limit")
		}
		expanded += f.UncompressedSize64
	}
	cleanDst := filepath.Clean(dst)
	for _, f := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !f.Mode().IsRegular() && !f.FileInfo().IsDir() {
			return fmt.Errorf("skillmarket: unsupported zip entry %q", f.Name)
		}
		if strings.Contains(f.Name, "\\") {
			return fmt.Errorf("skillmarket: illegal zip entry %q", f.Name)
		}
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("skillmarket: illegal zip entry %q", f.Name)
		}
		target := filepath.Join(cleanDst, name)
		if !strings.HasPrefix(target, cleanDst+string(os.PathSeparator)) && target != cleanDst {
			return fmt.Errorf("skillmarket: illegal zip entry %q", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyZipEntry(ctx, f, target); err != nil {
			return err
		}
	}
	return nil
}

func copyZipEntry(ctx context.Context, f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, &contextReader{ctx: ctx, reader: io.LimitReader(src, maxPackageSize+1)})
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func writeManifest(dir string, record InstallRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), data, 0o644)
}

// readManifest loads the manifest from an installed skill directory.
func readManifest(dir string) (InstallRecord, error) {
	info, err := os.Lstat(filepath.Join(dir, manifestName))
	if err != nil || !info.Mode().IsRegular() {
		return InstallRecord{}, ErrNotMarketplace
	}
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return InstallRecord{}, ErrNotMarketplace
		}
		return InstallRecord{}, err
	}
	var record InstallRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return InstallRecord{}, fmt.Errorf("skillmarket: corrupt manifest: %w", err)
	}
	if record.Name != filepath.Base(dir) || record.ID <= 0 {
		return InstallRecord{}, ErrNotMarketplace
	}
	return record, nil
}

// HasManifest reports whether dir contains a marketplace manifest.
func HasManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, manifestName))
	return err == nil
}

// Installed describes one marketplace-owned local skill.
type Installed struct {
	InstallRecord
	Dir string `json:"dir"`
}

// ListInstalled scans the skills root for manifest-bearing directories.
// Non-manifest directories (hand-written skills) are skipped silently.
func (c *Client) ListInstalled() ([]Installed, error) {
	entries, err := os.ReadDir(c.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Installed{}, nil
		}
		return nil, err
	}
	result := []Installed{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(c.skillsDir, entry.Name())
		record, err := readManifest(dir)
		if err != nil {
			continue // hand-written or corrupt — not our business
		}
		result = append(result, Installed{InstallRecord: record, Dir: dir})
	}
	return result, nil
}

// Uninstall removes a marketplace-installed skill by its directory name.
// A missing directory yields ErrNotFound; a directory without a manifest
// yields ErrNotMarketplace so user-written skills can never be deleted
// through the marketplace path.
func (c *Client) Uninstall(name string) error {
	c.mutation.Lock()
	defer c.mutation.Unlock()
	if !packageNameRe.MatchString(name) {
		return fmt.Errorf("skillmarket: invalid skill name %q", name)
	}
	dir := filepath.Join(c.skillsDir, name)
	if info, err := os.Lstat(dir); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrNotMarketplace
	}
	if _, err := readManifest(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
