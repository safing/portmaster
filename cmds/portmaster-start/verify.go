package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
	portlog "github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/service/updates/helper"
)

var (
	verifyVerbose bool
	verifyFix     bool

	verifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Check integrity of updates / components",
		RunE: func(cmd *cobra.Command, args []string) error {
			return verifyUpdates(cmd.Context())
		},
	}
)

func init() {
	rootCmd.AddCommand(verifyCmd)

	flags := verifyCmd.Flags()
	flags.BoolVarP(&verifyVerbose, "verbose", "v", false, "Enable verbose output")
	flags.BoolVar(&verifyFix, "fix", false, "Delete and re-download broken components")
}

func verifyUpdates(ctx context.Context) error {
	// Force registry to require signatures for all enabled scopes.
	for _, opts := range registry.Verification {
		if opts != nil {
			opts.DownloadPolicy = updater.SignaturePolicyRequire
			opts.DiskLoadPolicy = updater.SignaturePolicyRequire
		}
	}

	// Load indexes again to ensure they are correctly signed.
	err := registry.LoadIndexes(ctx)
	if err != nil {
		if verifyFix {
			log.Println("[WARN] loading indexes failed, re-downloading...")
			err = registry.UpdateIndexes(ctx)
			if err != nil {
				return fmt.Errorf("failed to download indexes: %w", err)
			}
			log.Println("[ OK ] indexes re-downloaded and verified")
		} else {
			return fmt.Errorf("failed to verify indexes: %w", err)
		}
	} else {
		log.Println("[ OK ] indexes verified")
	}

	// Verify all resources.
	export := registry.Export()
	var verified, fails, skipped int
	for _, rv := range export {
		for _, version := range rv.Versions {
			// Don't verify files we don't have.
			if !version.Available {
				continue
			}

			// Verify file signature.
			file := version.GetFile()
			fileData, err := file.Verify()
			switch {
			case err == nil:
				verified++
				if verifyVerbose {
					verifOpts := registry.GetVerificationOptions(file.Identifier())
					if verifOpts != nil {
						log.Printf(
							"[ OK ] valid signature for %s: signed by %s",
							file.Path(), getSignedByMany(fileData, verifOpts.TrustStore),
						)
					} else {
						log.Printf("[ OK ] valid signature for %s", file.Path())
					}
				}

			case errors.Is(err, updater.ErrVerificationNotConfigured):
				skipped++
				if verifyVerbose {
					log.Printf("[SKIP] no verification configured for %s", file.Path())
				}

			default:
				log.Printf("[FAIL] failed to verify %s: %s", file.Path(), err)
				fails++
				if verifyFix {
					// Delete file.
					err = os.Remove(file.Path())
					if err != nil && !errors.Is(err, fs.ErrNotExist) {
						log.Printf("[FAIL] failed to delete %s to prepare re-download: %s", file.Path(), err)
					} else {
						// We should not be changing the version, but we are in a cmd-like
						// scenario here without goroutines.
						version.Available = false
					}
					// Delete file sig.
					err = os.Remove(file.Path() + filesig.Extension)
					if err != nil && !errors.Is(err, fs.ErrNotExist) {
						log.Printf("[FAIL] failed to delete %s to prepare re-download: %s", file.Path()+filesig.Extension, err)
					} else {
						// We should not be changing the version, but we are in a cmd-like
						// scenario here without goroutines.
						version.SigAvailable = false
					}
				}
			}
		}
	}

	if verified > 0 {
		log.Printf("[STAT] verified %d files", verified)
	}
	if skipped > 0 && verifyVerbose {
		log.Printf("[STAT] skipped %d files (no verification configured)", skipped)
	}
	if fails > 0 {
		if verifyFix {
			log.Printf("[WARN] verification failed on %d files, re-downloading...", fails)
		} else {
			return fmt.Errorf("failed to verify %d files", fails)
		}
	} else {
		// Everything was verified!
		return nil
	}

	// Start logging system for update process.
	portlog.SetLogLevel(portlog.InfoLevel)
	err = portlog.Start()
	if err != nil {
		log.Printf("[WARN] failed to start logging for monitoring update process: %s\n", err)
	}
	defer portlog.Shutdown()

	// Re-download broken files.
	registry.MandatoryUpdates = helper.MandatoryUpdates()
	registry.AutoUnpack = helper.AutoUnpackUpdates()
	err = registry.DownloadUpdates(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to re-download files: %w", err)
	}

	return nil
}

func getSignedByMany(fds []*filesig.FileData, trustStore jess.TrustStore) string {
	signedBy := make([]string, 0, len(fds))
	for _, fd := range fds {
		if sig := fd.Signature(); sig != nil {
			for _, seal := range sig.Signatures {
				if signet, err := trustStore.GetSignet(seal.ID, true); err == nil {
					signedBy = append(signedBy, fmt.Sprintf("%s (%s)", signet.Info.Name, seal.ID))
				} else {
					signedBy = append(signedBy, seal.ID)
				}
			}
		}
	}
	return strings.Join(signedBy, " and ")
}
