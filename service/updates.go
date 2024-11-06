package service

import (
	"path/filepath"
	go_runtime "runtime"

	"github.com/safing/jess"
	"github.com/safing/portmaster/service/updates"
)

var (
	DefaultBinaryIndexURLs = []string{
		"https://updates.safing.io/stable.v3.json",
	}
	DefaultIntelIndexURLs = []string{
		"https://updates.safing.io/intel.v3.json",
	}

	// BinarySigningKeys holds the signing keys in text format.
	BinarySigningKeys = []string{
		// Safing Code Signing Key #1
		"recipient:public-ed25519-key:safing-code-signing-key-1:92bgBLneQUWrhYLPpBDjqHbpFPuNVCPAaivQ951A4aq72HcTiw7R1QmPJwFM1mdePAvEVDjkeb8S4fp2pmRCsRa8HrCvWQEjd88rfZ6TznJMfY4g7P8ioGFjfpyx2ZJ8WCZJG5Qt4Z9nkabhxo2Nbi3iywBTYDLSbP5CXqi7jryW7BufWWuaRVufFFzhwUC2ryWFWMdkUmsAZcvXwde4KLN9FrkWAy61fGaJ8GCwGnGCSitANnU2cQrsGBXZzxmzxwrYD",
		// Safing Code Signing Key #2
		"recipient:public-ed25519-key:safing-code-signing-key-2:92bgBLneQUWrhYLPpBDjqHbPC2d1o5JMyZFdavWBNVtdvbPfzDewLW95ScXfYPHd3QvWHSWCtB4xpthaYWxSkK1kYiGp68DPa2HaU8yQ5dZhaAUuV4Kzv42pJcWkCeVnBYqgGBXobuz52rFqhDJy3rz7soXEmYhJEJWwLwMeioK3VzN3QmGSYXXjosHMMNC76rjufSoLNtUQUWZDSnHmqbuxbKMCCsjFXUGGhtZVyb7bnu7QLTLk6SKHBJDMB6zdL9sw3",
	}

	// BinarySigningTrustStore is an in-memory trust store with the signing keys.
	BinarySigningTrustStore = jess.NewMemTrustStore()
)

func init() {
	for _, signingKey := range BinarySigningKeys {
		rcpt, err := jess.RecipientFromTextFormat(signingKey)
		if err != nil {
			panic(err)
		}
		err = BinarySigningTrustStore.StoreSignet(rcpt)
		if err != nil {
			panic(err)
		}
	}
}

func MakeUpdateConfigs(svcCfg *ServiceConfig) (binaryUpdateConfig, intelUpdateConfig *updates.Config, err error) {
	switch go_runtime.GOOS {
	case "windows":
		binaryUpdateConfig = &updates.Config{
			Name:              "binaries",
			Directory:         svcCfg.BinDir,
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_binaries"),
			PurgeDirectory:    filepath.Join(svcCfg.BinDir, "upgrade_obsolete_binaries"),
			Ignore:            []string{"databases", "intel", "config.json"},
			IndexURLs:         svcCfg.BinariesIndexURLs,
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyBinaryUpdates,
			AutoDownload:      false,
			AutoApply:         false,
			NeedsRestart:      true,
			Notify:            true,
		}
		intelUpdateConfig = &updates.Config{
			Name:              "intel",
			Directory:         filepath.Join(svcCfg.DataDir, "intel"),
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_intel"),
			PurgeDirectory:    filepath.Join(svcCfg.DataDir, "upgrade_obsolete_intel"),
			IndexURLs:         svcCfg.IntelIndexURLs,
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyIntelUpdates,
			AutoDownload:      true,
			AutoApply:         true,
			NeedsRestart:      false,
			Notify:            false,
		}

	case "linux":
		binaryUpdateConfig = &updates.Config{
			Name:              "binaries",
			Directory:         svcCfg.BinDir,
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_binaries"),
			PurgeDirectory:    filepath.Join(svcCfg.DataDir, "upgrade_obsolete_binaries"),
			Ignore:            []string{"databases", "intel", "config.json"},
			IndexURLs:         svcCfg.BinariesIndexURLs,
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyBinaryUpdates,
			AutoDownload:      false,
			AutoApply:         false,
			NeedsRestart:      true,
			Notify:            true,
		}
		intelUpdateConfig = &updates.Config{
			Name:              "intel",
			Directory:         filepath.Join(svcCfg.DataDir, "intel"),
			DownloadDirectory: filepath.Join(svcCfg.DataDir, "download_intel"),
			PurgeDirectory:    filepath.Join(svcCfg.DataDir, "upgrade_obsolete_intel"),
			IndexURLs:         svcCfg.IntelIndexURLs,
			IndexFile:         "index.json",
			Verify:            svcCfg.VerifyIntelUpdates,
			AutoDownload:      true,
			AutoApply:         true,
			NeedsRestart:      false,
			Notify:            false,
		}
	}

	return
}
