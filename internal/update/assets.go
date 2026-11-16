package update

import (
	"context"
	"fmt"
	"strings"
)

// Exported building blocks for other self-updaters, such as hrdx Desktop.
// They share the CLI's download, checksum, and replacement code paths.

// Download fetches url to dst.
func Download(ctx context.Context, url, dst string) error { return downloadFile(ctx, url, dst) }

// VerifyChecksum checks file against the sha256 that a checksums file at
// sumsPath lists for asset.
func VerifyChecksum(sumsPath, asset, file string) error {
	want, err := lookupChecksum(sumsPath, asset)
	if err != nil {
		return err
	}
	got, err := sha256File(file)
	if err != nil {
		return fmt.Errorf("hash %s: %w", asset, err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	return nil
}

// ReplaceExecutable installs newBin over cur. On Windows the running image
// is kept as <cur>.old until the next update.
func ReplaceExecutable(cur, newBin string) error { return replaceBinary(cur, newBin) }

// DownloadBase converts a release page URL into the base URL of its assets.
func DownloadBase(releaseURL string) string {
	base := strings.TrimSuffix(releaseURL, "/")
	return strings.Replace(base, "/releases/tag/", "/releases/download/", 1)
}
