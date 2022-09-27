package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
	"github.com/safing/portbase/updater"
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
	var verified, fails int
	for _, rv := range export {
		for _, version := range rv.Versions {
			file := version.GetFile()

			// Verify file signature.
			fileData, err := file.Verify()
			if err != nil {
				log.Printf("[FAIL] failed to verify %s: %s\n", file.Path(), err)
				fails++
				if verifyFix {
					// Delete file.
					err = os.Remove(file.Path())
					if err != nil {
						log.Printf("[FAIL] failed to delete %s to prepare re-download: %s\n", file.Path(), err)
					}
					// Delete file sig.
					err = os.Remove(file.Path() + filesig.Extension)
					if err != nil {
						log.Printf("[FAIL] failed to delete %s to prepare re-download: %s\n", file.Path()+filesig.Extension, err)
					}
				}
			} else {
				if verifyVerbose {
					verifOpts := registry.GetVerificationOptions(file.Identifier())
					if verifOpts != nil {
						log.Printf(
							"[ OK ] valid signature for %s: signed by %s\n",
							file.Path(), getSignedByMany(fileData, verifOpts.TrustStore),
						)
					} else {
						log.Printf("[ OK ] valid signature for %s\n", file.Path())
					}
				}
				verified++
			}
		}
	}

	if verified > 0 {
		log.Printf("[STAT] verified %d files\n", verified)
	}
	if fails > 0 {
		if verifyFix {
			log.Printf("[WARN] verification failed %d files, re-downloading...\n", fails)
		} else {
			return fmt.Errorf("failed to verify %d files", fails)
		}
	} else {
		// Everything was verified!
		return nil
	}

	// Re-download broken files.
	err = registry.DownloadUpdates(ctx)
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
