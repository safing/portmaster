package main

import (
	"bytes"
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
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/service/updates"
)

var (
	// UserAgent is an HTTP User-Agent that is used to add
	// more context to requests made by the registry when
	// fetching resources from the update server.
	UserAgent = fmt.Sprintf("Portmaster Update Mgr (%s %s)", runtime.GOOS, runtime.GOARCH)

	client http.Client
)

func init() {
	rootCmd.AddCommand(mirrorCmd)
}

var (
	mirrorCmd = &cobra.Command{
		Use:   "mirror [index URL] [mirror dir]",
		Short: "Mirror all artifacts by an index to a directory, keeping the directory structure and file names intact",
		RunE:  mirror,
		Args:  cobra.ExactArgs(2),
	}
)

func mirror(cmd *cobra.Command, args []string) error {
	// Args.
	indexURL := args[0]
	targetDir := args[1]

	// Check target dir.
	stat, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("failed to access target dir: %w", err)
	}
	if !stat.IsDir() {
		return errors.New("target is not a directory")
	}

	// Calculate Base URL.
	u, err := url.Parse(indexURL)
	if err != nil {
		return fmt.Errorf("invalid index URL: %w", err)
	}
	indexPath := u.Path
	u.RawQuery = ""
	u.RawFragment = ""
	u.Path = ""
	u.RawPath = ""
	baseURL := u.String() + "/"

	// Download Index.
	fmt.Println("downloading index...")
	indexData, err := downloadData(cmd.Context(), indexURL)
	if err != nil {
		return fmt.Errorf("download index: %w", err)
	}

	// Parse (and convert) index.
	var index *updates.Index
	_, newIndexName := path.Split(indexPath)
	switch {
	case strings.HasSuffix(indexPath, ".v3.json"):
		index = &updates.Index{}
		err := json.Unmarshal(indexData, index)
		if err != nil {
			return fmt.Errorf("parse v3 index: %w", err)
		}
	case strings.HasSuffix(indexPath, ".v2.json"):
		index, err = convertV2(indexData, baseURL)
		if err != nil {
			return fmt.Errorf("convert v2 index: %w", err)
		}
		newIndexName = strings.TrimSuffix(newIndexName, ".v2.json") + ".v3.json"
	case strings.HasSuffix(indexPath, ".json"):
		index, err = convertV1(indexData, baseURL, time.Now())
		if err != nil {
			return fmt.Errorf("convert v1 index: %w", err)
		}
		newIndexName = strings.TrimSuffix(newIndexName, ".json") + ".v3.json"
	default:
		return errors.New("invalid index file extension")
	}

	// Download and save artifacts.
	for _, artifact := range index.Artifacts {
		fmt.Printf("downloading %s...\n", artifact.Filename)

		// Download artifact and add any missing checksums.
		artifactData, artifactLocation, err := getArtifact(cmd.Context(), artifact)
		if err != nil {
			return fmt.Errorf("get artifact %s: %w", artifact.Filename, err)
		}

		// Write artifact to correct location.
		artifactDst := filepath.Join(targetDir, filepath.FromSlash(artifactLocation))
		artifactDir, _ := filepath.Split(artifactDst)
		err = os.MkdirAll(artifactDir, 0o0755)
		if err != nil {
			return fmt.Errorf("create artifact dir %s: %w", artifactDir, err)
		}
		err = os.WriteFile(artifactDst, artifactData, 0o0644)
		if err != nil {
			return fmt.Errorf("save artifact %s: %w", artifact.Filename, err)
		}
	}

	// Save index.
	indexJson, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	indexDst := filepath.Join(targetDir, newIndexName)
	err = os.WriteFile(indexDst, indexJson, 0o0644)
	if err != nil {
		return fmt.Errorf("write index to %s: %w", indexDst, err)
	}

	return err
}

func getArtifact(ctx context.Context, artifact *updates.Artifact) (artifactData []byte, artifactLocation string, err error) {
	// Check URL.
	if len(artifact.URLs) == 0 {
		return nil, "", errors.New("no URLs defined")
	}
	u, err := url.Parse(artifact.URLs[0])
	if err != nil {
		return nil, "", fmt.Errorf("invalid URL: %w", err)
	}

	// Download data from URL.
	artifactData, err = downloadData(ctx, artifact.URLs[0])
	if err != nil {
		return nil, "", fmt.Errorf("GET artifact: %w", err)
	}

	// Decompress artifact data, if configured.
	var finalArtifactData []byte
	if artifact.Unpack != "" {
		finalArtifactData, err = updates.Decompress(artifact.Unpack, artifactData)
		if err != nil {
			return nil, "", fmt.Errorf("decompress: %w", err)
		}
	} else {
		finalArtifactData = artifactData
	}

	// Verify or generate checksum.
	if artifact.SHA256 != "" {
		if err := updates.CheckSHA256Sum(finalArtifactData, artifact.SHA256); err != nil {
			return nil, "", err
		}
	} else {
		fileHash := sha256.New()
		if _, err := io.Copy(fileHash, bytes.NewReader(finalArtifactData)); err != nil {
			return nil, "", fmt.Errorf("digest file: %w", err)
		}
		artifact.SHA256 = hex.EncodeToString(fileHash.Sum(nil))
	}

	return artifactData, u.Path, nil
}

func downloadData(ctx context.Context, url string) ([]byte, error) {
	// Setup request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request to %s: %w", url, err)
	}
	if UserAgent != "" {
		req.Header.Set("User-Agent", UserAgent)
	}

	// Start request with shared http client.
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed a get file request to: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for HTTP status errors.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned non-OK status: %d %s", resp.StatusCode, resp.Status)
	}

	// Read the full body and return it.
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body of response: %w", err)
	}
	return content, nil
}
