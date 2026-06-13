package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func ensureDataFile(filePath, downloadURL string) error {
	if _, err := os.Stat(filePath); err == nil {
		slog.Info("data file exists, skipping download", "path", filePath)
		return nil
	}
	slog.Info("downloading GeoNames data", "url", downloadURL)

	destDir := filepath.Dir(filePath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", destDir, err)
	}

	zipPath := filepath.Join(destDir, "download.zip")
	if err := downloadFile(zipPath, downloadURL); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	fmt.Println()
	slog.Info("download complete, extracting")

	if err := extractZip(zipPath, destDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	_ = os.Remove(zipPath)

	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("expected file not found after extraction: %s", filePath)
	}
	slog.Info("data file ready", "path", filePath)
	return nil
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("http.Get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer f.Close()
	wc := &writeCounter{}
	_, err = io.Copy(f, io.TeeReader(resp.Body, wc))
	return err
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("zip.OpenReader: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		lower := strings.ToLower(name)
		if f.FileInfo().IsDir() || !strings.HasSuffix(lower, ".txt") || strings.Contains(lower, "readme") {
			continue
		}
		outPath := filepath.Join(destDir, name)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc) //nolint:gosec
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		slog.Info("extracted", "file", outPath)
	}
	return nil
}

type writeCounter struct{ total int64 }

func (wc *writeCounter) Write(p []byte) (int, error) {
	wc.total += int64(len(p))
	fmt.Printf("\rDownloaded %.1f MB", float64(wc.total)/1024/1024)
	return len(p), nil
}
