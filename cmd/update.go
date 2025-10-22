package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kedare/compass/internal/logger"
	"github.com/kedare/compass/internal/update"
	"github.com/kedare/compass/internal/version"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var (
	updateCheck bool
	updateForce bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the compass binary to the latest GitHub release",
	Long: `Fetch the most recent GitHub release for kedare/compass and replace the
current executable with the matching binary for your platform.

Use --check to verify whether an update is available without downloading it,
or --force to reinstall the latest release even when already up to date.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		info := version.Get()
		currentVersion := info.Version

		client := &http.Client{
			Timeout: 2 * time.Minute,
		}

		manager := update.NewManager(
			client,
			"kedare",
			"compass",
			update.WithUserAgent(fmt.Sprintf("compass/%s (%s)", strings.TrimSpace(currentVersion), info.BuildArch)),
		)

		releaseSpinner, _ := pterm.DefaultSpinner.Start("Checking for updates...")
		release, err := manager.LatestRelease(ctx)
		if err != nil {
			releaseSpinner.Fail("Failed to fetch release metadata")
			return err
		}

		releaseSpinner.Success(fmt.Sprintf("Latest release: %s", release.TagName))

		updateAvailable := update.ShouldUpdate(currentVersion, release.TagName)

		if updateCheck && updateForce {
			pterm.Warning.Printf("--force ignored when --check is set because no update will be downloaded.\n")
		}

		if updateCheck {
			if !updateAvailable {
				pterm.Info.Printf("No updates available. Current version: %s, latest release: %s.\n", currentVersion, release.TagName)
				return nil
			}

			if _, err := update.AssetForCurrentPlatform(release); err != nil {
				if errors.Is(err, update.ErrNoMatchingAsset) {
					return fmt.Errorf("update available (%s) but no asset is published for %s/%s", release.TagName, runtime.GOOS, runtime.GOARCH)
				}
				return err
			}

			pterm.Success.Printf("Update available: %s -> %s\n", currentVersion, release.TagName)
			return nil
		}

		if !updateAvailable && !updateForce {
			pterm.Info.Printf("You are already running the latest version (%s).\n", currentVersion)
			return nil
		}

		if updateForce && !updateAvailable {
			pterm.Warning.Printf("Forcing reinstall of version %s (current version: %s).\n", release.TagName, currentVersion)
		} else {
			pterm.Info.Printf("Updating from %s to %s\n", currentVersion, release.TagName)
		}

		asset, err := update.AssetForCurrentPlatform(release)
		if err != nil {
			if errors.Is(err, update.ErrNoMatchingAsset) {
				return fmt.Errorf("no release asset available for %s/%s", runtime.GOOS, runtime.GOARCH)
			}

			return err
		}

		logger.Log.Debugf("Selected asset %s (%d bytes)", asset.Name, asset.Size)

		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("determine executable path: %w", err)
		}

		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("resolve symlinks for executable: %w", err)
		}

		parentDir := filepath.Dir(execPath)
		tmpFile, err := os.CreateTemp(parentDir, "compass-update-*")
		if err != nil {
			return fmt.Errorf("create temporary file: %w", err)
		}

		tmpPath := tmpFile.Name()
		shouldCleanup := true
		defer func() {
			if tmpFile != nil {
				if closeErr := tmpFile.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
					logger.Log.Warnf("Failed to close temp file %s: %v", tmpPath, closeErr)
				}
			}
			if shouldCleanup {
				if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
					logger.Log.Warnf("Failed to remove temp file %s: %v", tmpPath, err)
				}
			}
		}()

		pterm.Info.Printf("Downloading %s...\n", asset.Name)

		resp, err := manager.DownloadAsset(ctx, asset)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				logger.Log.Warnf("Failed to close download stream: %v", closeErr)
			}
		}()

		progressBarBuilder := pterm.DefaultProgressbar.WithTitle(fmt.Sprintf("Downloading %s", asset.Name))
		if asset.Size > 0 {
			if asset.Size > math.MaxInt {
				progressBarBuilder = progressBarBuilder.WithTotal(math.MaxInt)
			} else {
				progressBarBuilder = progressBarBuilder.WithTotal(int(asset.Size))
			}
		}

		progressBar, _ := progressBarBuilder.Start()

		writer := &progressReporter{bar: progressBar}
		if _, err = io.Copy(tmpFile, io.TeeReader(resp.Body, writer)); err != nil {
			if progressBar != nil {
				if _, stopErr := progressBar.Stop(); stopErr != nil {
					logger.Log.Warnf("Failed to stop progress bar: %v", stopErr)
				}
			}
			return fmt.Errorf("write download to disk: %w", err)
		}

		if progressBar != nil {
			if _, stopErr := progressBar.Stop(); stopErr != nil {
				logger.Log.Warnf("Failed to stop progress bar: %v", stopErr)
			}
		}

		if err := tmpFile.Sync(); err != nil {
			return fmt.Errorf("flush temporary file: %w", err)
		}

		if runtime.GOOS != "windows" {
			if err := tmpFile.Chmod(0o755); err != nil {
				return fmt.Errorf("set executable permissions: %w", err)
			}
		}

		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("close temporary file: %w", err)
		}
		tmpFile = nil

		switch runtime.GOOS {
		case "windows":
			newPath := prepareWindowsNewPath(execPath)
			if err := copyFile(tmpPath, newPath); err != nil {
				return fmt.Errorf("prepare replacement binary: %w", err)
			}

			pterm.Success.Printf("Downloaded new binary to %s\n", newPath)
			pterm.Warning.Printf("Automatic replacement is not supported on Windows.\nA new binary has been written to %s.\nPlease replace %s with this file after closing the current process.\n", newPath, execPath)
		default:
			if err := os.Rename(tmpPath, execPath); err != nil {
				return fmt.Errorf("replace executable: %w", err)
			}
			shouldCleanup = false
			pterm.Success.Printf("Updated compass to %s\n", release.TagName)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "Only check if a newer release exists without downloading it")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "Reinstall the latest release even if already up to date")
}

// progressReporter adapts io.Writer to bridge download progress with a pterm progress bar.
type progressReporter struct {
	bar *pterm.ProgressbarPrinter
}

// Write updates the underlying progress bar as data flows through the TeeReader.
func (p *progressReporter) Write(data []byte) (int, error) {
	if p.bar != nil {
		p.bar.Add(len(data))
	}

	return len(data), nil
}

// prepareWindowsNewPath returns a companion path that Windows users can manually promote after exiting the process.
func prepareWindowsNewPath(execPath string) string {
	ext := filepath.Ext(execPath)
	base := strings.TrimSuffix(execPath, ext)
	if ext == "" {
		return execPath + ".new"
	}

	return base + ".new" + ext
}

// copyFile copies src to dst, replacing any existing file contents.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil {
			logger.Log.Warnf("Failed to close source file %s: %v", src, closeErr)
		}
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil {
			logger.Log.Warnf("Failed to close destination file %s: %v", dst, closeErr)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}
