package updates

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/safing/portmaster/base/log"
)

const MaxUnpackSize = 1 << 30 // 2^30 == 1GB

// {
//   "Bundle": "Portmaster Intel",
//   "Version": "2024",
//   "Published": "2024-06-13T14:46:28Z",
//   "Artifacts": [
//     {
//       "Filename": "assets.zip",
//       "SHA256": "1e3454f448b478460d5f3a12ebd450b896c6548b00e279ae12c04c14f2c07d82",
//       "URLs": ["https://updates.safing.io/all/ui/modules/assets_v0-3-2.zip"]
//     },
//     {
//       "Filename": "portmaster.zip",
//       "SHA256": "cdddfa4bd54183902f0660f1c7aa3104ce9684f54604ad3654641cfd1972a287",
//       "URLs": ["https://updates.safing.io/all/ui/modules/portmaster_v0-8-7.zip"]
//     },
//     {
//       "Filename": "ui",
//       "SHA256": "f6b44bfc8dbd2ad36ef003b3d311d3e004f4b37b7fe245f83778400a5b9bcb85",
//       "URLs": ["https://updates.safing.io/linux_amd64/app/portmaster-app_v0-2-8.zip"],
//       "Platform": "linux_amd64",
//       "Unpack": "zip",
//       "Directory": true
//     },
//     {
//       "Filename": "portmaster-core",
//       "SHA256": "1683b6310b868b348f54ea242ba7e8c2835ac236efb2337d29a353bc0e7671b0",
//       "URLs": ["https://updates.safing.io/linux_amd64/core/portmaster-core_v1-6-12"],
//       "Platform": "linux_amd64"
//     },
//     {
//       "Filename": "portmaster-notifier",
//       "SHA256": "69cf14eeb1b9422cd05dfeff53c5447a424af7cb1c3e1de63bfdb4c6182cb096",
//       "URLs": ["https://updates.safing.io/linux_amd64/notifier/portmaster-notifier_v0-3-6"],
//       "Platform": "linux_amd64"
//     },
//     {
//       "Filename": "portmaster-start",
//       "SHA256": "c6778c579fad6a8d25f904ea9863d18fb690585afc4f92c45a1a2bb436262f58",
//       "URLs": ["https://updates.safing.io/linux_amd64/start/portmaster-start_v1-6-0"],
//       "Platform": "linux_amd64"
//     }
//   ]
// }

// {
//   "Bundle": "Portmaster Binaries",
//   "Version": "20240529.0.1",
//   "Published": "2024-06-13T14:46:28Z",
//   "Artifacts": [
//     {
//       "Filename": "all/intel/geoip/geoipv4.mmdb",
//       "SHA256": "86c5f3045aa9cb81ee5d07d6231e3f88fb4f873bcdf6ca532e0fe157614701fe",
//       "URLs": ["https://updates.safing.io/all/intel/geoip/geoipv4_v20240529-0-1.mmdb.gz"],
//       "Unpack": "gz"
//     },
//     {
//       "Filename": "all/intel/geoip/geoipv6.mmdb",
//       "SHA256": "e9883312fc25a2cbcbd84f936077b88a6998e92fa1728a1ad0f72392ea7df79c",
//       "URLs": ["https://updates.safing.io/all/intel/geoip/geoipv6_v20240529-0-1.mmdb.gz"],
//       "Unpack": "gz"
//     }
//   ]
// }

type Artifact struct {
	Filename string   `json:"Filename"`
	SHA256   string   `json:"SHA256"`
	URLs     []string `json:"URLs"`
	Platform string   `json:"Platform,omitempty"`
	Unpack   string   `json:"Unpack,omitempty"`
	Version  string   `json:"Version,omitempty"`
}

type Bundle struct {
	Name      string     `json:"Bundle"`
	Version   string     `json:"Version"`
	Published time.Time  `json:"Published"`
	Artifacts []Artifact `json:"Artifacts"`
}

func fetchBundle(info UpdaterInfo) (Bundle, error) {
	var bundle Bundle
	var err error
	// Most of the time only the first url is used, rest are just for fallback.
request_loop:
	for _, url := range info.IndexURLs {
		// Fetch data
		client := http.Client{}
		var resp *http.Response
		resp, err = client.Get(url)
		if err != nil {
			err = fmt.Errorf("failed a get request to %s: %w", url, err)
			log.Errorf("updates: %s", err)
			// Try with the next url
			continue
		}
		var body []byte
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			err = fmt.Errorf("failed to read body of request to %s: %w", url, err)
			log.Errorf("updates: %s", err)
			// Try with the next url
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		err = json.Unmarshal(body, &bundle)
		if err != nil {
			err = fmt.Errorf("failed to parse body of the response of %s: %w", url, err)
			log.Errorf("updates: %s", err)
			// Try with the next url
			continue
		}

		// Fetching was successful. Reset error and return.
		err = nil
		break request_loop
	}

	return bundle, err
}

func downloadAndVerify(bundle Bundle, dataDir string) {
	client := http.Client{}
	for _, artifact := range bundle.Artifacts {

		downloadFile := fmt.Sprintf("%s/%s", dataDir, artifact.Filename)
		_ = os.MkdirAll(filepath.Dir(downloadFile), os.ModePerm)

		err := processArtifact(&client, artifact, downloadFile)
		if err != nil {
			log.Errorf("updates: %s", err)
		}
	}
}

func processArtifact(client *http.Client, artifact Artifact, filePath string) error {
	providedHash, err := hex.DecodeString(artifact.SHA256)
	if err != nil || len(providedHash) != sha256.Size {
		return fmt.Errorf("invalid provided hash %s: %w", artifact.SHA256, err)
	}

	// Download
	content, err := downloadFile(client, artifact.URLs)
	if err != nil {
		return fmt.Errorf("failed to download artifact: %w", err)
	}

	// Decompress
	if artifact.Unpack != "" {
		content, err = unpack(artifact.Unpack, content)
		if err != nil {
			return fmt.Errorf("failed to decompress artifact: %w", err)
		}
	}

	// Verify
	hash := sha256.Sum256(content)
	if !bytes.Equal(providedHash, hash[:]) {
		// FIXME(vladimir): just for testing. Make it an error before commit.
		err = fmt.Errorf("failed to verify artifact: %s", artifact.Filename)
		log.Debugf("updates: %s", err)
	}

	// Save
	tmpFilename := fmt.Sprintf("%s.download", filePath)
	file, err := os.Create(tmpFilename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	_, err = file.Write(content)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	// Rename
	err = os.Rename(tmpFilename, filePath)
	if err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

func downloadFile(client *http.Client, urls []string) ([]byte, error) {
	for _, url := range urls {
		// Try to make the request
		resp, err := client.Get(url)
		if err != nil {
			log.Warningf("failed a get file request to: %s", err)
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		// Check if the server returned an error
		if resp.StatusCode != http.StatusOK {
			log.Warningf("server returned non-OK status: %d %s", resp.StatusCode, resp.Status)
			continue
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Warningf("failed to read body of response: %s", err)
			continue
		}
		return content, nil
	}

	return nil, fmt.Errorf("failed to download file from the provided urls")
}

func unpack(cType string, fileBytes []byte) ([]byte, error) {
	switch cType {
	case "zip":
		{
			return decompressZip(fileBytes)
		}
	case "gz":
		{
			return decompressGzip(fileBytes)
		}
	default:
		{
			return nil, fmt.Errorf("unsupported compression type")
		}
	}
}

func decompressGzip(data []byte) ([]byte, error) {
	// Create a gzip reader from the byte array
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	var buf bytes.Buffer
	_, err = io.CopyN(&buf, gzipReader, MaxUnpackSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read gzip file: %w", err)
	}

	return buf.Bytes(), nil
}

func decompressZip(data []byte) ([]byte, error) {
	// Create a zip reader from the byte array
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}

	// Ensure there is only one file in the zip
	if len(zipReader.File) != 1 {
		return nil, fmt.Errorf("zip file must contain exactly one file")
	}

	// Read the single file in the zip
	file := zipReader.File[0]
	fileReader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer func() { _ = fileReader.Close() }()

	var buf bytes.Buffer
	_, err = io.CopyN(&buf, fileReader, MaxUnpackSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read file in zip: %w", err)
	}

	return buf.Bytes(), nil
}
