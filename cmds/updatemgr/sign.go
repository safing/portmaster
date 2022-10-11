package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
	"github.com/safing/jess/truststores"
)

func init() {
	rootCmd.AddCommand(signCmd)

	// Required argument: envelope
	signCmd.PersistentFlags().StringVarP(&envelopeName, "envelope", "", "",
		"specify envelope name used for signing",
	)
	_ = signCmd.MarkFlagRequired("envelope")

	// Optional arguments: verbose, tsdir, tskeyring
	signCmd.PersistentFlags().BoolVarP(&signVerbose, "verbose", "v", false,
		"enable verbose output",
	)
	signCmd.PersistentFlags().StringVarP(&trustStoreDir, "tsdir", "", "",
		"specify a truststore directory (default loaded from JESS_TS_DIR env variable)",
	)
	signCmd.PersistentFlags().StringVarP(&trustStoreKeyring, "tskeyring", "", "",
		"specify a truststore keyring namespace (default loaded from JESS_TS_KEYRING env variable) - lower priority than tsdir",
	)

	// Subcommand for signing indexes.
	signCmd.AddCommand(signIndexCmd)
}

var (
	signCmd = &cobra.Command{
		Use:   "sign",
		Short: "Sign resources",
		RunE:  sign,
		Args:  cobra.NoArgs,
	}
	signIndexCmd = &cobra.Command{
		Use:   "index",
		Short: "Sign indexes",
		RunE:  signIndex,
		Args:  cobra.ExactArgs(1),
	}

	envelopeName string
	signVerbose  bool
)

func sign(cmd *cobra.Command, args []string) error {
	// Setup trust store.
	trustStore, err := setupTrustStore()
	if err != nil {
		return err
	}

	// Get envelope.
	signingEnvelope, err := trustStore.GetEnvelope(envelopeName)
	if err != nil {
		return err
	}

	// Get all resources and iterate over all versions.
	export := registry.Export()
	var verified, signed, fails int
	for _, rv := range export {
		for _, version := range rv.Versions {
			file := version.GetFile()

			// Check if there is an existing signature.
			_, err := os.Stat(file.Path() + filesig.Extension)
			switch {
			case err == nil || errors.Is(err, fs.ErrExist):
				// If the file exists, just verify.
				fileData, err := filesig.VerifyFile(
					file.Path(),
					file.Path()+filesig.Extension,
					file.SigningMetadata(),
					trustStore,
				)
				if err != nil {
					fmt.Printf("[FAIL] signature error for %s: %s\n", file.Path(), err)
					fails++
				} else {
					if signVerbose {
						fmt.Printf("[ OK ] valid signature for %s: signed by %s\n", file.Path(), getSignedByMany(fileData, trustStore))
					}
					verified++
				}

			case errors.Is(err, fs.ErrNotExist):
				// Attempt to sign file.
				fileData, err := filesig.SignFile(
					file.Path(),
					file.Path()+filesig.Extension,
					file.SigningMetadata(),
					signingEnvelope,
					trustStore,
				)
				if err != nil {
					fmt.Printf("[FAIL] failed to sign %s: %s\n", file.Path(), err)
					fails++
				} else {
					fmt.Printf("[SIGN] signed %s with %s\n", file.Path(), getSignedBySingle(fileData, trustStore))
					signed++
				}

			default:
				// File access error.
				fmt.Printf("[FAIL] failed to access %s: %s\n", file.Path(), err)
				fails++
			}
		}
	}

	if verified > 0 {
		fmt.Printf("[STAT] verified %d files\n", verified)
	}
	if signed > 0 {
		fmt.Printf("[STAT] signed %d files\n", signed)
	}
	if fails > 0 {
		return fmt.Errorf("signing or verification failed on %d files", fails)
	}
	return nil
}

func signIndex(cmd *cobra.Command, args []string) error {
	// Setup trust store.
	trustStore, err := setupTrustStore()
	if err != nil {
		return err
	}

	// Get envelope.
	signingEnvelope, err := trustStore.GetEnvelope(envelopeName)
	if err != nil {
		return err
	}

	// Resolve globs.
	files := make([]string, 0, len(args))
	for _, arg := range args {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return err
		}
		files = append(files, matches...)
	}

	// Go through all files.
	var verified, signed, fails int
	for _, file := range files {
		sigFile := file + filesig.Extension

		// Ignore matches for the signatures.
		if strings.HasSuffix(file, filesig.Extension) {
			continue
		}

		// Check if there is an existing signature.
		_, err := os.Stat(sigFile)
		switch {
		case err == nil || errors.Is(err, fs.ErrExist):
			// If the file exists, just verify.
			fileData, err := filesig.VerifyFile(
				file,
				sigFile,
				nil,
				trustStore,
			)
			if err == nil {
				if signVerbose {
					fmt.Printf("[ OK ] valid signature for %s: signed by %s\n", file, getSignedByMany(fileData, trustStore))
				}
				verified++

				// Indexes are expected to change, so just sign the index again if verification fails.
				continue
			}

			fallthrough
		case errors.Is(err, fs.ErrNotExist):
			// Attempt to sign file.
			fileData, err := filesig.SignFile(
				file,
				sigFile,
				nil,
				signingEnvelope,
				trustStore,
			)
			if err != nil {
				fmt.Printf("[FAIL] failed to sign %s: %s\n", file, err)
				fails++
			} else {
				fmt.Printf("[SIGN] signed %s with %s\n", file, getSignedBySingle(fileData, trustStore))
				signed++
			}

		default:
			// File access error.
			fmt.Printf("[FAIL] failed to access %s: %s\n", sigFile, err)
			fails++
		}
	}

	if verified > 0 {
		fmt.Printf("[STAT] verified %d files", verified)
	}
	if signed > 0 {
		fmt.Printf("[STAT] signed %d files", signed)
	}
	if fails > 0 {
		return fmt.Errorf("signing failed on %d files", fails)
	}
	return nil
}

var (
	trustStoreDir     string
	trustStoreKeyring string
)

func setupTrustStore() (trustStore truststores.ExtendedTrustStore, err error) {
	// Get trust store directory.
	if trustStoreDir == "" {
		trustStoreDir, _ = os.LookupEnv("JESS_TS_DIR")
		if trustStoreDir == "" {
			trustStoreDir, _ = os.LookupEnv("JESS_TSDIR")
		}
	}
	if trustStoreDir != "" {
		trustStore, err = truststores.NewDirTrustStore(trustStoreDir)
		if err != nil {
			return nil, err
		}
	}

	// Get trust store keyring.
	if trustStore == nil {
		if trustStoreKeyring == "" {
			trustStoreKeyring, _ = os.LookupEnv("JESS_TS_KEYRING")
			if trustStoreKeyring == "" {
				trustStoreKeyring, _ = os.LookupEnv("JESS_TSKEYRING")
			}
		}
		if trustStoreKeyring != "" {
			trustStore, err = truststores.NewKeyringTrustStore(trustStoreKeyring)
			if err != nil {
				return nil, err
			}
		}
	}

	// Truststore is mandatory.
	if trustStore == nil {
		return nil, errors.New("no truststore configured, please pass arguments or use env variables")
	}

	return trustStore, nil
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

func getSignedBySingle(fd *filesig.FileData, trustStore jess.TrustStore) string {
	if sig := fd.Signature(); sig != nil {
		signedBy := make([]string, 0, len(sig.Signatures))
		for _, seal := range sig.Signatures {
			if signet, err := trustStore.GetSignet(seal.ID, true); err == nil {
				signedBy = append(signedBy, fmt.Sprintf("%s (%s)", signet.Info.Name, seal.ID))
			} else {
				signedBy = append(signedBy, seal.ID)
			}
		}
		return strings.Join(signedBy, " and ")
	}

	return ""
}
