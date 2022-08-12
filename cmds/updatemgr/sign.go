package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
	"github.com/safing/jess/truststores"
	"github.com/safing/portbase/formats/dsd"
	"github.com/safing/portbase/updater"
)

const letterFileExtension = ".letter"

func init() {
	rootCmd.AddCommand(signCmd)
	signCmd.PersistentFlags().StringVarP(&envelopeName, "envelope", "", "",
		"specify envelope name used for signing",
	)
	_ = signCmd.MarkFlagRequired("envelope")
	signCmd.PersistentFlags().StringVarP(&trustStoreDir, "tsdir", "", "",
		"specify a truststore directory (default loaded from JESS_TS_DIR env variable)",
	)
	signCmd.PersistentFlags().StringVarP(&trustStoreKeyring, "tskeyring", "", "",
		"specify a truststore keyring namespace (default loaded from JESS_TS_KEYRING env variable) - lower priority than tsdir",
	)
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
	var fails int
	for _, rv := range export {
		for _, version := range rv.Versions {
			file := version.GetFile()

			// Check if there is an existing signature.
			_, err := os.Stat(file.Path() + filesig.Extension)
			switch {
			case err == nil || os.IsExist(err):
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
					fmt.Printf("[ OK ] valid signature for %s: signed by %s\n", file.Path(), getSignedByMany(fileData, trustStore))
				}

			case os.IsNotExist(err):
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
				}

			default:
				// File access error.
				fmt.Printf("[FAIL] failed to access %s: %s\n", file.Path(), err)
				fails++
			}
		}
	}

	if fails > 0 {
		return fmt.Errorf("signing or checking failed on %d files", fails)
	}
	return nil
}

func signIndex(cmd *cobra.Command, args []string) error {
	// FIXME:
	// Do not sign embedded, but also as a separate file.
	// Slightly more complex, but it makes all the other handling easier.

	indexFilePath := args[0]

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

	// Read index file.
	indexData, err := ioutil.ReadFile(indexFilePath)
	if err != nil {
		return fmt.Errorf("failed to read index file %s: %w", indexFilePath, err)
	}

	// Load index.
	resourceVersions := make(map[string]string)
	err = json.Unmarshal(indexData, &resourceVersions)
	if err != nil {
		return fmt.Errorf("failed to parse index file: %w", err)
	}

	// Create signed index file structure.
	index := updater.IndexFile{
		Channel:   strings.TrimSuffix(filepath.Base(indexFilePath), filepath.Ext(indexFilePath)),
		Published: time.Now(),
		Expires:   time.Now().Add(3 * 31 * 24 * time.Hour), // Expires in 3 Months.
		Versions:  resourceVersions,
	}

	// Serialize index.
	indexData, err = dsd.Dump(index, dsd.CBOR)
	if err != nil {
		return fmt.Errorf("failed to serialize index structure: %w", err)
	}

	// Sign index.
	session, err := signingEnvelope.Correspondence(trustStore)
	if err != nil {
		return fmt.Errorf("failed to prepare signing: %w", err)
	}
	signedIndex, err := session.Close(indexData)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	// Write new file.
	signedIndexData, err := signedIndex.ToDSD(dsd.CBOR)
	if err != nil {
		return fmt.Errorf("failed to serialize signed index: %w", err)
	}
	signedIndexFilePath := strings.TrimSuffix(indexFilePath, filepath.Ext(indexFilePath)) + letterFileExtension
	err = ioutil.WriteFile(signedIndexFilePath, signedIndexData, 0o644) //nolint:gosec // Permission is ok.
	if err != nil {
		return fmt.Errorf("failed to write signed index to %s: %w", signedIndexFilePath, err)
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
