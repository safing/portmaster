package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
	"github.com/safing/jess/truststores"
	"github.com/safing/portmaster/service/updates"
)

func init() {
	rootCmd.AddCommand(signCmd)

	// Required argument: envelope
	signCmd.Flags().StringVarP(&envelopeName, "envelope", "", "",
		"specify envelope name used for signing",
	)
	_ = signCmd.MarkFlagRequired("envelope")

	// Optional arguments: verbose, tsdir, tskeyring
	signCmd.Flags().BoolVarP(&signVerbose, "verbose", "v", false,
		"enable verbose output",
	)
	signCmd.Flags().StringVarP(&trustStoreDir, "tsdir", "", "",
		"specify a truststore directory (default loaded from JESS_TS_DIR env variable)",
	)
	signCmd.Flags().StringVarP(&trustStoreKeyring, "tskeyring", "", "",
		"specify a truststore keyring namespace (default loaded from JESS_TS_KEYRING env variable) - lower priority than tsdir",
	)
}

var (
	signCmd = &cobra.Command{
		Use:   "sign [index.json file]",
		Short: "Sign an index",
		RunE:  sign,
		Args:  cobra.ExactArgs(1),
	}

	envelopeName string
	signVerbose  bool
)

func sign(cmd *cobra.Command, args []string) error {
	indexFilename := args[0]

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

	// Read index file from disk.
	unsignedIndexData, err := os.ReadFile(indexFilename)
	if err != nil {
		return fmt.Errorf("read index file: %w", err)
	}

	// Parse index and check if it is valid.
	index, err := updates.ParseIndex(unsignedIndexData, nil)
	if err != nil {
		return fmt.Errorf("invalid index: %w", err)
	}
	err = index.CanDoUpgrades()
	if err != nil {
		return fmt.Errorf("invalid index: %w", err)
	}

	// Sign index.
	signedIndexData, err := filesig.AddJSONSignature(unsignedIndexData, signingEnvelope, trustStore)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	// Check by parsing again.
	index, err = updates.ParseIndex(signedIndexData, nil)
	if err != nil {
		return fmt.Errorf("invalid index after signing: %w", err)
	}
	err = index.CanDoUpgrades()
	if err != nil {
		return fmt.Errorf("invalid index after signing: %w", err)
	}

	// Write back to file.
	err = os.WriteFile(indexFilename, signedIndexData, 0o0644)
	if err != nil {
		return fmt.Errorf("write signed index file: %w", err)
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
