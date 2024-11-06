package updates

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	semver "github.com/hashicorp/go-version"
	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
)

// MaxUnpackSize defines the maximum size that is allowed to be unpacked.
const MaxUnpackSize = 1 << 30 // 2^30 == 1GB

const currentPlatform = runtime.GOOS + "_" + runtime.GOARCH

var zeroVersion = semver.Must(semver.NewVersion("0.0.0"))

// Artifacts represents a single file with metadata.
type Artifact struct {
	Filename string   `json:"Filename"`
	SHA256   string   `json:"SHA256"`
	URLs     []string `json:"URLs"`
	Platform string   `json:"Platform,omitempty"`
	Unpack   string   `json:"Unpack,omitempty"`
	Version  string   `json:"Version,omitempty"`

	localFile  string
	versionNum *semver.Version
}

// GetFileMode returns the required filesystem permission for the artifact.
func (a *Artifact) GetFileMode() os.FileMode {
	// Special case for portmaster ui. Should be able to be executed from the regular user
	if a.Platform == currentPlatform && a.Filename == "portmaster" {
		return executableUIFileMode
	}

	if a.Platform == currentPlatform {
		return executableFileMode
	}

	return defaultFileMode
}

// Path returns the absolute path to the local file.
func (a *Artifact) Path() string {
	return a.localFile
}

// SemVer returns the version of the artifact.
func (a *Artifact) SemVer() *semver.Version {
	return a.versionNum
}

// IsNewerThan returns whether the artifact is newer than the given artifact.
// Returns true if the given artifact is nil.
// The second return value "ok" is false when version could not be compared.
// In this case, it is up to the caller to decide how to proceed.
func (a *Artifact) IsNewerThan(b *Artifact) (newer, ok bool) {
	switch {
	case a == nil:
		return false, false
	case b == nil:
		return true, true
	case a.versionNum == nil:
		return false, false
	case b.versionNum == nil:
		return false, false
	case a.versionNum.GreaterThan(b.versionNum):
		return true, true
	default:
		return false, true
	}
}

func (a *Artifact) export(dir string, indexVersion *semver.Version) *Artifact {
	copy := &Artifact{
		Filename:   a.Filename,
		SHA256:     a.SHA256,
		URLs:       a.URLs,
		Platform:   a.Platform,
		Unpack:     a.Unpack,
		Version:    a.Version,
		localFile:  filepath.Join(dir, a.Filename),
		versionNum: a.versionNum,
	}

	// Make sure we have a version number.
	switch {
	case copy.versionNum != nil:
		// Version already parsed.
	case copy.Version != "":
		// Need to parse version.
		v, err := semver.NewVersion(copy.Version)
		if err == nil {
			copy.versionNum = v
		}
	default:
		// No version defined, inherit index version.
		copy.versionNum = indexVersion
	}

	return copy
}

// Index represents a collection of artifacts with metadata.
type Index struct {
	Name      string     `json:"Name"`
	Version   string     `json:"Version"`
	Published time.Time  `json:"Published"`
	Artifacts []Artifact `json:"Artifacts"`

	versionNum *semver.Version
}

// LoadIndex loads and parses an index from the given filename.
func LoadIndex(filename string, trustStore jess.TrustStore) (*Index, error) {
	// Read index file from disk.
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read index file: %w", err)
	}

	// Parse and return.
	return ParseIndex(content, trustStore)
}

// ParseIndex parses an index from a json string.
func ParseIndex(jsonContent []byte, trustStore jess.TrustStore) (*Index, error) {
	// Verify signature.
	if trustStore != nil {
		if err := filesig.VerifyJSONSignature(jsonContent, trustStore); err != nil {
			return nil, fmt.Errorf("verify: %w", err)
		}
	}

	// Parse json.
	var index Index
	err := json.Unmarshal([]byte(jsonContent), &index)
	if err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}

	// Initialize data.
	err = index.init()
	if err != nil {
		return nil, err
	}

	return &index, nil
}

func (index *Index) init() error {
	// Parse version number, if set.
	if index.Version != "" {
		versionNum, err := semver.NewVersion(index.Version)
		if err != nil {
			return fmt.Errorf("invalid index version %q: %w", index.Version, err)
		}
		index.versionNum = versionNum
	}

	// Filter artifacts by current platform.
	filtered := make([]Artifact, 0)
	for _, a := range index.Artifacts {
		if a.Platform == "" || a.Platform == currentPlatform {
			filtered = append(filtered, a)
		}
	}
	index.Artifacts = filtered

	// Parse artifact version numbers.
	for _, a := range index.Artifacts {
		if a.Version != "" {
			v, err := semver.NewVersion(a.Version)
			if err == nil {
				a.versionNum = v
			}
		} else {
			a.versionNum = index.versionNum
		}
	}

	return nil
}

// CanDoUpgrades returns whether the index is able to follow a secure upgrade path.
func (index *Index) CanDoUpgrades() error {
	switch {
	case index.versionNum == nil:
		return errors.New("missing version number")

	case index.Published.IsZero():
		return errors.New("missing publish date")

	case index.Published.After(time.Now().Add(15 * time.Minute)):
		return fmt.Errorf("is from the future (%s)", time.Until(index.Published).Round(time.Minute))

	default:
		return nil
	}
}

// ShouldUpgradeTo returns whether the given index is a successor and should be upgraded to.
func (index *Index) ShouldUpgradeTo(newIndex *Index) error {
	// Check if both indexes can do upgrades.
	if err := index.CanDoUpgrades(); err != nil {
		return fmt.Errorf("current index cannot do upgrades: %w", err)
	}
	if err := newIndex.CanDoUpgrades(); err != nil {
		return fmt.Errorf("new index cannot do upgrade: %w")
	}

	switch {
	case index.versionNum.Equal(zeroVersion):
		// The zero version is used for bootstrapping.
		// Upgrade in any case.
		return nil

	case index.Name != newIndex.Name:
		return errors.New("index names do not match")

	case index.versionNum.GreaterThan(newIndex.versionNum):
		return errors.New("current index has newer version")

	case index.Published.After(newIndex.Published):
		return errors.New("current index was published later")

	case index.Published.Equal(newIndex.Published):
		// "Do nothing".
		return ErrSameIndex

	default:
		// Upgrade!
		return nil
	}
}

// VerifyArtifacts checks if all artifacts are present in the given dir and have the correct hash.
func (index *Index) VerifyArtifacts(dir string) error {
	for _, artifact := range index.Artifacts {
		err := checkSHA256SumFile(filepath.Join(dir, artifact.Filename), artifact.SHA256)
		if err != nil {
			return fmt.Errorf("verify %s: %s", artifact.Filename, err)
		}
	}

	return nil
}

func (index *Index) Export(signingKey *jess.Signet, trustStore jess.TrustStore) ([]byte, error) {
	// Serialize to json.
	indexData, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("serialize: %w", err)
	}

	// Do not sign if signing key is not given.
	if signingKey == nil {
		return indexData, nil
	}

	// Make envelope.
	envelope := jess.NewUnconfiguredEnvelope()
	envelope.SuiteID = jess.SuiteSignV1
	envelope.Senders = []*jess.Signet{signingKey}

	// Sign json data.
	signedIndex, err := filesig.AddJSONSignature(indexData, envelope, trustStore)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	return signedIndex, nil
}

func checkSHA256SumFile(filename string, sha256sum string) error {
	// Check expected hash.
	expectedDigest, err := hex.DecodeString(sha256sum)
	if err != nil {
		return fmt.Errorf("invalid hex encoding for expected hash %s: %w", sha256sum, err)
	}
	if len(expectedDigest) != sha256.Size {
		return fmt.Errorf("invalid size for expected hash %s: %w", sha256sum, err)
	}

	// Open file for checking.
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Calculate hash of the file.
	fileHash := sha256.New()
	if _, err := io.Copy(fileHash, file); err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if subtle.ConstantTimeCompare(fileHash.Sum(nil), expectedDigest) != 1 {
		return errors.New("sha256sum mismatch")
	}

	return nil
}

func checkSHA256Sum(fileData []byte, sha256sum string) error {
	// Check expected hash.
	expectedDigest, err := hex.DecodeString(sha256sum)
	if err != nil {
		return fmt.Errorf("invalid hex encoding for expected hash %s: %w", sha256sum, err)
	}
	if len(expectedDigest) != sha256.Size {
		return fmt.Errorf("invalid size for expected hash %s: %w", sha256sum, err)
	}

	// Calculate and compare hash of the file.
	hashSum := sha256.Sum256(fileData)
	if subtle.ConstantTimeCompare(hashSum[:], expectedDigest) != 1 {
		return errors.New("sha256sum mismatch")
	}

	return nil
}

// copyAndCheckSHA256Sum copies the file from src to dst and check the sha256 sum.
// As a special case, if the sha256sum is not given, it is not checked.
func copyAndCheckSHA256Sum(src, dst, sha256sum string, fileMode fs.FileMode) error {
	// Check expected hash.
	var expectedDigest []byte
	if sha256sum != "" {
		expectedDigest, err := hex.DecodeString(sha256sum)
		if err != nil {
			return fmt.Errorf("invalid hex encoding for expected hash %s: %w", sha256sum, err)
		}
		if len(expectedDigest) != sha256.Size {
			return fmt.Errorf("invalid size for expected hash %s: %w", sha256sum, err)
		}
	}

	// Read file from source.
	fileData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read src file: %w", err)
	}

	// Calculate and compare hash of the file.
	if len(expectedDigest) > 0 {
		hashSum := sha256.Sum256(fileData)
		if subtle.ConstantTimeCompare(hashSum[:], expectedDigest) != 1 {
			return errors.New("sha256sum mismatch")
		}
	}

	// Write to temporary file.
	tmpDst := dst + ".copy"
	err = os.WriteFile(tmpDst, fileData, fileMode)
	if err != nil {
		return fmt.Errorf("write temp dst file: %w", err)
	}

	// Rename/Move to actual location.
	err = os.Rename(tmpDst, dst)
	if err != nil {
		return fmt.Errorf("rename dst file after write: %w", err)
	}

	return nil
}
