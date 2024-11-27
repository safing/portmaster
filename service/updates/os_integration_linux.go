package updates

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tevino/abool"
	"golang.org/x/exp/slices"

	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils/renameio"
)

var (
	portmasterCoreServiceFilePath     = "portmaster.service"
	portmasterNotifierServiceFilePath = "portmaster_notifier.desktop"
	backupExtension                   = ".backup"

	//go:embed assets/portmaster.service
	currentPortmasterCoreServiceFile []byte

	checkedSystemIntegration = abool.New()

	// ErrRequiresManualUpgrade is returned when a system integration file requires a manual upgrade.
	ErrRequiresManualUpgrade = errors.New("requires a manual upgrade")
)

func upgradeSystemIntegration() {
	// Check if we already checked the system integration.
	if !checkedSystemIntegration.SetToIf(false, true) {
		return
	}

	// Upgrade portmaster core systemd service.
	err := upgradeSystemIntegrationFile(
		"portmaster core systemd service",
		filepath.Join(dataroot.Root().Path, portmasterCoreServiceFilePath),
		0o0600,
		currentPortmasterCoreServiceFile,
		[]string{
			"bc26dd37e6953af018ad3676ee77570070e075f2b9f5df6fa59d65651a481468", // Commit 19c76c7 on 2022-01-25
			"cc0cb49324dfe11577e8c066dd95cc03d745b50b2153f32f74ca35234c3e8cb5", // Commit ef479e5 on 2022-01-24
			"d08a3b5f3aee351f8e120e6e2e0a089964b94c9e9d0a9e5fa822e60880e315fd", // Commit b64735e on 2021-12-07
		},
	)
	if err != nil {
		log.Warningf("updates: %s", err)
		return
	}

	// Upgrade portmaster notifier systemd user service.
	// Permissions only!
	err = upgradeSystemIntegrationFile(
		"portmaster notifier systemd user service",
		filepath.Join(dataroot.Root().Path, portmasterNotifierServiceFilePath),
		0o0644,
		nil, // Do not update contents.
		nil, // Do not update contents.
	)
	if err != nil {
		log.Warningf("updates: %s", err)
		return
	}
}

// upgradeSystemIntegrationFile upgrades the file contents and permissions.
// System integration files are not necessarily present and may also be
// edited by third parties, such as the OS itself or other installers.
// The supplied hashes must be sha256 hex-encoded.
func upgradeSystemIntegrationFile(
	name string,
	filePath string,
	fileMode fs.FileMode,
	fileData []byte,
	permittedUpgradeHashes []string,
) error {
	// Upgrade file contents.
	if len(fileData) > 0 {
		if err := upgradeSystemIntegrationFileContents(name, filePath, fileData, permittedUpgradeHashes); err != nil {
			return err
		}
	}

	// Upgrade file permissions.
	if fileMode != 0 {
		if err := upgradeSystemIntegrationFilePermissions(name, filePath, fileMode); err != nil {
			return err
		}
	}

	return nil
}

// upgradeSystemIntegrationFileContents upgrades the file contents.
// System integration files are not necessarily present and may also be
// edited by third parties, such as the OS itself or other installers.
// The supplied hashes must be sha256 hex-encoded.
func upgradeSystemIntegrationFileContents(
	name string,
	filePath string,
	fileData []byte,
	permittedUpgradeHashes []string,
) error {
	// Read existing file.
	existingFileData, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read %s at %s: %w", name, filePath, err)
	}

	// Check if file is already the current version.
	existingSum := sha256.Sum256(existingFileData)
	existingHexSum := hex.EncodeToString(existingSum[:])
	currentSum := sha256.Sum256(fileData)
	currentHexSum := hex.EncodeToString(currentSum[:])
	if existingHexSum == currentHexSum {
		log.Debugf("updates: %s at %s is up to date", name, filePath)
		return nil
	}

	// Check if we are allowed to upgrade from the existing file.
	if !slices.Contains[[]string, string](permittedUpgradeHashes, existingHexSum) {
		return fmt.Errorf("%s at %s (sha256:%s) %w, as it is not a previously published version and cannot be automatically upgraded - try installing again", name, filePath, existingHexSum, ErrRequiresManualUpgrade)
	}

	// Start with upgrade!

	// Make backup of existing file.
	err = CopyFile(filePath, filePath+backupExtension)
	if err != nil {
		return fmt.Errorf(
			"failed to create backup of %s from %s to %s: %w",
			name,
			filePath,
			filePath+backupExtension,
			err,
		)
	}

	// Open destination file for writing.
	atomicDstFile, err := renameio.TempFile(registry.TmpDir().Path, filePath)
	if err != nil {
		return fmt.Errorf("failed to create tmp file to update %s at %s: %w", name, filePath, err)
	}
	defer atomicDstFile.Cleanup() //nolint:errcheck // ignore error for now, tmp dir will be cleaned later again anyway

	// Write file.
	_, err = io.Copy(atomicDstFile, bytes.NewReader(fileData))
	if err != nil {
		return err
	}

	// Finalize file.
	err = atomicDstFile.CloseAtomicallyReplace()
	if err != nil {
		return fmt.Errorf("failed to finalize update of %s at %s: %w", name, filePath, err)
	}

	log.Warningf("updates: %s at %s was upgraded to %s - a reboot may be required", name, filePath, currentHexSum)
	return nil
}

// upgradeSystemIntegrationFilePermissions upgrades the file permissions.
// System integration files are not necessarily present and may also be
// edited by third parties, such as the OS itself or other installers.
func upgradeSystemIntegrationFilePermissions(
	name string,
	filePath string,
	fileMode fs.FileMode,
) error {
	// Get current file permissions.
	stat, err := os.Stat(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read %s file metadata at %s: %w", name, filePath, err)
	}

	// If permissions are as expected, do nothing.
	if stat.Mode().Perm() == fileMode {
		return nil
	}

	// Otherwise, set correct permissions.
	err = os.Chmod(filePath, fileMode)
	if err != nil {
		return fmt.Errorf("failed to update %s file permissions at %s: %w", name, filePath, err)
	}

	log.Warningf("updates: %s file permissions at %s updated to %v", name, filePath, fileMode)
	return nil
}
