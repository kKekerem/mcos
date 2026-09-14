// Package catalog is a small client for the Modrinth API (https://modrinth.com),
// used to search and install mods, plugins, datapacks, and resource packs. It is
// key-free and public; MCOS uses it both for the panel's catalog browser and for
// auto-installing helper plugins like ViaVersion.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mcos/internal/version"
)

const apiBase = "https://api.modrinth.com/v2"

// userAgent is required by Modrinth's API etiquette.
const userAgent = version.UserAgent

// Project is a single Modrinth search hit.
type Project struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Downloads   int      `json:"downloads"`
	ProjectType string   `json:"project_type"`
	Categories  []string `json:"categories"`
	IconURL     string   `json:"icon_url,omitempty"`
}

type searchResp struct {
	Hits []Project `json:"hits"`
}

// File is a downloadable artifact attached to a project version.
type File struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
}

// Version is one published version of a project.
type Version struct {
	ID            string    `json:"id"`
	VersionNumber string    `json:"version_number"`
	GameVersions  []string  `json:"game_versions"`
	Loaders       []string  `json:"loaders"`
	Files         []File    `json:"files"`
	DatePublished time.Time `json:"date_published"`
}

// Client talks to the Modrinth API.
type Client struct {
	http *http.Client
	base string
}

// New constructs a catalog client with a sane timeout.
func New() *Client {
	return &Client{http: &http.Client{Timeout: 60 * time.Second}, base: apiBase}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("modrinth %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// facets JSON-encodes a set of single-value facet groups for the search query.
func facets(pairs ...string) string {
	groups := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		groups = append(groups, fmt.Sprintf("[%q]", p))
	}
	if len(groups) == 0 {
		return ""
	}
	return "[" + strings.Join(groups, ",") + "]"
}

// Search finds projects matching query, optionally constrained by project type
// ("mod","plugin","datapack","resourcepack" — Modrinth uses these strings),
// loader (e.g. "paper","fabric"), and game version (e.g. "1.21.1").
func (c *Client) Search(ctx context.Context, query, projectType, loader, gameVersion string, limit int) ([]Project, error) {
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("index", "relevance")
	var fp []string
	if projectType != "" {
		fp = append(fp, "project_type:"+projectType)
	}
	if loader != "" {
		fp = append(fp, "categories:"+loader)
	}
	if gameVersion != "" {
		fp = append(fp, "versions:"+gameVersion)
	}
	if f := facets(fp...); f != "" {
		q.Set("facets", f)
	}
	var res searchResp
	if err := c.get(ctx, "/search", q, &res); err != nil {
		return nil, err
	}
	return res.Hits, nil
}

// Versions lists a project's versions filtered by loader and game version
// (both optional), newest first.
func (c *Client) Versions(ctx context.Context, idOrSlug, loader, gameVersion string) ([]Version, error) {
	q := url.Values{}
	if loader != "" {
		q.Set("loaders", fmt.Sprintf("[%q]", loader))
	}
	if gameVersion != "" {
		q.Set("game_versions", fmt.Sprintf("[%q]", gameVersion))
	}
	var vs []Version
	if err := c.get(ctx, "/project/"+url.PathEscape(idOrSlug)+"/version", q, &vs); err != nil {
		return nil, err
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].DatePublished.After(vs[j].DatePublished) })
	return vs, nil
}

// BestFile resolves the newest compatible primary file for a project.
func (c *Client) BestFile(ctx context.Context, idOrSlug, loader, gameVersion string) (File, error) {
	vs, err := c.Versions(ctx, idOrSlug, loader, gameVersion)
	if err != nil {
		return File{}, err
	}
	for _, v := range vs {
		for _, f := range v.Files {
			if f.Primary {
				return f, nil
			}
		}
		if len(v.Files) > 0 {
			return v.Files[0], nil
		}
	}
	return File{}, fmt.Errorf("no compatible file for %q (loader=%s, mc=%s)", idOrSlug, loader, gameVersion)
}

// DownloadTo downloads file into destDir, returning the written path. The
// destination directory is created if needed.
func (c *Client) DownloadTo(ctx context.Context, file File, destDir string) (string, error) {
	if file.URL == "" {
		return "", fmt.Errorf("empty file url")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", file.Filename, resp.Status)
	}
	name := filepath.Base(file.Filename)
	if name == "" || name == "." || name == "/" {
		name = "download.jar"
	}
	dest := filepath.Join(destDir, name)
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}
	return dest, nil
}

// InstallByID searches by exact slug/id and installs the best compatible file
// into destDir. Convenience wrapper used for known helper projects.
func (c *Client) InstallByID(ctx context.Context, idOrSlug, loader, gameVersion, destDir string) (string, error) {
	f, err := c.BestFile(ctx, idOrSlug, loader, gameVersion)
	if err != nil {
		return "", err
	}
	return c.DownloadTo(ctx, f, destDir)
}
