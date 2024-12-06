package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/safing/jess/filesig"
	"github.com/safing/jess/lhash"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/base/utils/renameio"
)

func (reg *ResourceRegistry) fetchFile(ctx context.Context, client *http.Client, rv *ResourceVersion, tries int) error {
	// backoff when retrying
	if tries > 0 {
		select {
		case <-ctx.Done():
			return nil // module is shutting down
		case <-time.After(time.Duration(tries*tries) * time.Second):
		}
	}

	// check destination dir
	dirPath := filepath.Dir(rv.storagePath())
	err := reg.storageDir.EnsureAbsPath(dirPath)
	if err != nil {
		return fmt.Errorf("could not create updates folder: %s", dirPath)
	}

	// If verification is enabled, download signature first.
	var (
		verifiedHash *lhash.LabeledHash
		sigFileData  []byte
	)
	if rv.resource.VerificationOptions != nil {
		verifiedHash, sigFileData, err = reg.fetchAndVerifySigFile(
			ctx, client,
			rv.resource.VerificationOptions,
			rv.versionedSigPath(), rv.SigningMetadata(),
			tries,
		)
		if err != nil {
			switch rv.resource.VerificationOptions.DownloadPolicy {
			case SignaturePolicyRequire:
				return fmt.Errorf("signature verification failed: %w", err)
			case SignaturePolicyWarn:
				log.Warningf("%s: failed to verify downloaded signature of %s: %s", reg.Name, rv.versionedPath(), err)
			case SignaturePolicyDisable:
				log.Debugf("%s: failed to verify downloaded signature of %s: %s", reg.Name, rv.versionedPath(), err)
			}
		}
	}

	// open file for writing
	atomicFile, err := renameio.TempFile(reg.tmpDir.Path, rv.storagePath())
	if err != nil {
		return fmt.Errorf("could not create temp file for download: %w", err)
	}
	defer atomicFile.Cleanup() //nolint:errcheck // ignore error for now, tmp dir will be cleaned later again anyway

	// start file download
	resp, downloadURL, err := reg.makeRequest(ctx, client, rv.versionedPath(), tries)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Write to the hasher at the same time, if needed.
	var hasher hash.Hash
	var writeDst io.Writer = atomicFile
	if verifiedHash != nil {
		hasher = verifiedHash.Algorithm().RawHasher()
		writeDst = io.MultiWriter(hasher, atomicFile)
	}

	// Download and write file.
	n, err := io.Copy(writeDst, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to download %q: %w", downloadURL, err)
	}
	if resp.ContentLength != n {
		return fmt.Errorf("failed to finish download of %q: written %d out of %d bytes", downloadURL, n, resp.ContentLength)
	}

	// Before file is finalized, check if hash, if available.
	if hasher != nil {
		downloadDigest := hasher.Sum(nil)
		if verifiedHash.EqualRaw(downloadDigest) {
			log.Infof("%s: verified signature of %s", reg.Name, downloadURL)
		} else {
			switch rv.resource.VerificationOptions.DownloadPolicy {
			case SignaturePolicyRequire:
				return errors.New("file does not match signed checksum")
			case SignaturePolicyWarn:
				log.Warningf("%s: checksum does not match file from %s", reg.Name, downloadURL)
			case SignaturePolicyDisable:
				log.Debugf("%s: checksum does not match file from %s", reg.Name, downloadURL)
			}

			// Reset hasher to signal that the sig should not be written.
			hasher = nil
		}
	}

	// Write signature file, if we have one and if verification succeeded.
	if len(sigFileData) > 0 && hasher != nil {
		sigFilePath := rv.storagePath() + filesig.Extension
		err := os.WriteFile(sigFilePath, sigFileData, 0o0644) //nolint:gosec
		if err != nil {
			switch rv.resource.VerificationOptions.DownloadPolicy {
			case SignaturePolicyRequire:
				return fmt.Errorf("failed to write signature file %s: %w", sigFilePath, err)
			case SignaturePolicyWarn:
				log.Warningf("%s: failed to write signature file %s: %s", reg.Name, sigFilePath, err)
			case SignaturePolicyDisable:
				log.Debugf("%s: failed to write signature file %s: %s", reg.Name, sigFilePath, err)
			}
		}
	}

	// finalize file
	err = atomicFile.CloseAtomicallyReplace()
	if err != nil {
		return fmt.Errorf("%s: failed to finalize file %s: %w", reg.Name, rv.storagePath(), err)
	}
	// set permissions
	// TODO: distinguish between executable and non executable files.
	err = utils.SetExecPermission(rv.storagePath(), utils.PublicReadPermission)
	if err != nil {
		log.Warningf("%s: failed to set permissions on downloaded file %s: %s", reg.Name, rv.storagePath(), err)
	}

	log.Debugf("%s: fetched %s and stored to %s", reg.Name, downloadURL, rv.storagePath())
	return nil
}

func (reg *ResourceRegistry) fetchMissingSig(ctx context.Context, client *http.Client, rv *ResourceVersion, tries int) error {
	// backoff when retrying
	if tries > 0 {
		select {
		case <-ctx.Done():
			return nil // module is shutting down
		case <-time.After(time.Duration(tries*tries) * time.Second):
		}
	}

	// Check destination dir.
	dirPath := filepath.Dir(rv.storagePath())
	err := reg.storageDir.EnsureAbsPath(dirPath)
	if err != nil {
		return fmt.Errorf("could not create updates folder: %s", dirPath)
	}

	// Download and verify the missing signature.
	verifiedHash, sigFileData, err := reg.fetchAndVerifySigFile(
		ctx, client,
		rv.resource.VerificationOptions,
		rv.versionedSigPath(), rv.SigningMetadata(),
		tries,
	)
	if err != nil {
		switch rv.resource.VerificationOptions.DownloadPolicy {
		case SignaturePolicyRequire:
			return fmt.Errorf("signature verification failed: %w", err)
		case SignaturePolicyWarn:
			log.Warningf("%s: failed to verify downloaded signature of %s: %s", reg.Name, rv.versionedPath(), err)
		case SignaturePolicyDisable:
			log.Debugf("%s: failed to verify downloaded signature of %s: %s", reg.Name, rv.versionedPath(), err)
		}
		return nil
	}

	// Check if the signature matches the resource file.
	ok, err := verifiedHash.MatchesFile(rv.storagePath())
	if err != nil {
		switch rv.resource.VerificationOptions.DownloadPolicy {
		case SignaturePolicyRequire:
			return fmt.Errorf("error while verifying resource file: %w", err)
		case SignaturePolicyWarn:
			log.Warningf("%s: error while verifying resource file %s", reg.Name, rv.storagePath())
		case SignaturePolicyDisable:
			log.Debugf("%s: error while verifying resource file %s", reg.Name, rv.storagePath())
		}
		return nil
	}
	if !ok {
		switch rv.resource.VerificationOptions.DownloadPolicy {
		case SignaturePolicyRequire:
			return errors.New("resource file does not match signed checksum")
		case SignaturePolicyWarn:
			log.Warningf("%s: checksum does not match resource file from %s", reg.Name, rv.storagePath())
		case SignaturePolicyDisable:
			log.Debugf("%s: checksum does not match resource file from %s", reg.Name, rv.storagePath())
		}
		return nil
	}

	// Write signature file.
	err = os.WriteFile(rv.storageSigPath(), sigFileData, 0o0644) //nolint:gosec
	if err != nil {
		switch rv.resource.VerificationOptions.DownloadPolicy {
		case SignaturePolicyRequire:
			return fmt.Errorf("failed to write signature file %s: %w", rv.storageSigPath(), err)
		case SignaturePolicyWarn:
			log.Warningf("%s: failed to write signature file %s: %s", reg.Name, rv.storageSigPath(), err)
		case SignaturePolicyDisable:
			log.Debugf("%s: failed to write signature file %s: %s", reg.Name, rv.storageSigPath(), err)
		}
	}

	log.Debugf("%s: fetched %s and stored to %s", reg.Name, rv.versionedSigPath(), rv.storageSigPath())
	return nil
}

func (reg *ResourceRegistry) fetchAndVerifySigFile(ctx context.Context, client *http.Client, verifOpts *VerificationOptions, sigFilePath string, requiredMetadata map[string]string, tries int) (*lhash.LabeledHash, []byte, error) {
	// Download signature file.
	resp, _, err := reg.makeRequest(ctx, client, sigFilePath, tries)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	sigFileData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// Extract all signatures.
	sigs, err := filesig.ParseSigFile(sigFileData)
	switch {
	case len(sigs) == 0 && err != nil:
		return nil, nil, fmt.Errorf("failed to parse signature file: %w", err)
	case len(sigs) == 0:
		return nil, nil, errors.New("no signatures found in signature file")
	case err != nil:
		return nil, nil, fmt.Errorf("failed to parse signature file: %w", err)
	}

	// Verify all signatures.
	var verifiedHash *lhash.LabeledHash
	for _, sig := range sigs {
		fd, err := filesig.VerifyFileData(
			sig,
			requiredMetadata,
			verifOpts.TrustStore,
		)
		if err != nil {
			return nil, sigFileData, err
		}

		// Save or check verified hash.
		if verifiedHash == nil {
			verifiedHash = fd.FileHash()
		} else if !fd.FileHash().Equal(verifiedHash) {
			// Return an error if two valid hashes mismatch.
			// For simplicity, all hash algorithms must be the same for now.
			return nil, sigFileData, errors.New("file hashes from different signatures do not match")
		}
	}

	return verifiedHash, sigFileData, nil
}

func (reg *ResourceRegistry) fetchData(ctx context.Context, client *http.Client, downloadPath string, tries int) (fileData []byte, downloadedFrom string, err error) {
	// backoff when retrying
	if tries > 0 {
		select {
		case <-ctx.Done():
			return nil, "", nil // module is shutting down
		case <-time.After(time.Duration(tries*tries) * time.Second):
		}
	}

	// start file download
	resp, downloadURL, err := reg.makeRequest(ctx, client, downloadPath, tries)
	if err != nil {
		return nil, downloadURL, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// download and write file
	buf := bytes.NewBuffer(make([]byte, 0, resp.ContentLength))
	n, err := io.Copy(buf, resp.Body)
	if err != nil {
		return nil, downloadURL, fmt.Errorf("failed to download %q: %w", downloadURL, err)
	}
	if resp.ContentLength != n {
		return nil, downloadURL, fmt.Errorf("failed to finish download of %q: written %d out of %d bytes", downloadURL, n, resp.ContentLength)
	}

	return buf.Bytes(), downloadURL, nil
}

func (reg *ResourceRegistry) makeRequest(ctx context.Context, client *http.Client, downloadPath string, tries int) (resp *http.Response, downloadURL string, err error) {
	// parse update URL
	updateBaseURL := reg.UpdateURLs[tries%len(reg.UpdateURLs)]
	u, err := url.Parse(updateBaseURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse update URL %q: %w", updateBaseURL, err)
	}
	// add download path
	u.Path = path.Join(u.Path, downloadPath)
	// compile URL
	downloadURL = u.String()

	// create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request for %q: %w", downloadURL, err)
	}

	// set user agent
	if reg.UserAgent != "" {
		req.Header.Set("User-Agent", reg.UserAgent)
	}

	// start request
	resp, err = client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to make request to %q: %w", downloadURL, err)
	}

	// check return code
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, "", fmt.Errorf("failed to fetch %q: %d %s", downloadURL, resp.StatusCode, resp.Status)
	}

	return resp, downloadURL, err
}
