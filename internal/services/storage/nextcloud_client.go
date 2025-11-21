package storage

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// NextcloudClient is a lightweight WebDAV client for pushing files to Nextcloud.
type NextcloudClient struct {
	baseURL    *url.URL
	username   string
	password   string
	basePath   string
	httpClient *http.Client
}

func NewNextcloudClient(rawURL, username, password, basePath string) (*NextcloudClient, error) {
	parsed, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse nextcloud url: %w", err)
	}
	if basePath == "" {
		basePath = "rentalcore-filepool"
	}
	return &NextcloudClient{
		baseURL:    parsed,
		username:   username,
		password:   password,
		basePath:   strings.Trim(basePath, "/"),
		httpClient: &http.Client{},
	}, nil
}

// joinPath builds a full WebDAV URL for a given relative path.
func (c *NextcloudClient) joinPath(rel string) string {
	rel = strings.TrimLeft(rel, "/")
	full := path.Join(c.basePath, rel)
	u := *c.baseURL
	u.Path = path.Join(c.baseURL.Path, full)
	return u.String()
}

// EnsureCollections makes sure each segment exists via MKCOL.
func (c *NextcloudClient) EnsureCollections(rel string) error {
	rel = strings.TrimLeft(rel, "/")
	segments := strings.Split(rel, "/")
	current := ""
	for _, seg := range segments[:len(segments)-1] {
		if seg == "" {
			continue
		}
		current = path.Join(current, seg)
		req, err := http.NewRequest("MKCOL", c.joinPath(current), nil)
		if err != nil {
			return fmt.Errorf("mkcol request: %w", err)
		}
		req.SetBasicAuth(c.username, c.password)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("mkcol: %w", err)
		}
		// 201 Created or 405 Method Not Allowed if already exists
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed {
			resp.Body.Close()
			return fmt.Errorf("mkcol %s failed with status %s", current, resp.Status)
		}
		resp.Body.Close()
	}
	return nil
}

// Upload streams a file to Nextcloud at the given relative path.
func (c *NextcloudClient) Upload(rel string, body io.Reader, contentType string) error {
	if err := c.EnsureCollections(rel); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, c.joinPath(rel), body)
	if err != nil {
		return fmt.Errorf("put request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload failed: %s", resp.Status)
	}
	return nil
}

// Download fetches a file from Nextcloud.
func (c *NextcloudClient) Download(rel string) (io.ReadCloser, string, error) {
	req, err := http.NewRequest(http.MethodGet, c.joinPath(rel), nil)
	if err != nil {
		return nil, "", fmt.Errorf("get request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, "", fmt.Errorf("download failed: %s", resp.Status)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// Delete removes a file from Nextcloud.
func (c *NextcloudClient) Delete(rel string) error {
	req, err := http.NewRequest(http.MethodDelete, c.joinPath(rel), nil)
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete failed: %s", resp.Status)
	}
	return nil
}
