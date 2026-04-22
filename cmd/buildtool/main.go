package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type versionInfoFile struct {
	FixedFileInfo struct {
		FileVersion struct {
			Major int `json:"Major"`
			Minor int `json:"Minor"`
			Patch int `json:"Patch"`
			Build int `json:"Build"`
		} `json:"FileVersion"`
		ProductVersion struct {
			Major int `json:"Major"`
			Minor int `json:"Minor"`
			Patch int `json:"Patch"`
			Build int `json:"Build"`
		} `json:"ProductVersion"`
		FileFlagsMask string `json:"FileFlagsMask"`
		FileFlags     string `json:"FileFlags "`
		FileOS        string `json:"FileOS"`
		FileType      string `json:"FileType"`
		FileSubType   string `json:"FileSubType"`
	} `json:"FixedFileInfo"`
	StringFileInfo struct {
		Comments         string `json:"Comments"`
		CompanyName      string `json:"CompanyName"`
		FileDescription  string `json:"FileDescription"`
		FileVersion      string `json:"FileVersion"`
		InternalName     string `json:"InternalName"`
		LegalCopyright   string `json:"LegalCopyright"`
		LegalTrademarks  string `json:"LegalTrademarks"`
		OriginalFilename string `json:"OriginalFilename"`
		PrivateBuild     string `json:"PrivateBuild"`
		ProductName      string `json:"ProductName"`
		ProductVersion   string `json:"ProductVersion"`
		SpecialBuild     string `json:"SpecialBuild"`
	} `json:"StringFileInfo"`
	VarFileInfo struct {
		Translation struct {
			LangID    string `json:"LangID"`
			CharsetID string `json:"CharsetID"`
		} `json:"Translation"`
	} `json:"VarFileInfo"`
}

type buildTarget struct {
	PackagePath     string
	OutputName      string
	Description     string
	InternalName    string
	OriginalName    string
	VersionInfoJSON string
	VersionInfoSyso string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	versionPrefixBytes, err := os.ReadFile(filepath.Join(repoRoot, "VERSION"))
	if err != nil {
		return fmt.Errorf("read VERSION: %w", err)
	}
	versionPrefix := strings.TrimSpace(string(versionPrefixBytes))
	if !regexp.MustCompile(`^\d+\.\d+$`).MatchString(versionPrefix) {
		return fmt.Errorf("VERSION must contain major.minor, for example 1.0")
	}

	patch := 0
	if baselineCommit, err := runGitTrimmed(repoRoot, "log", "-n", "1", "--format=%H", "--", "VERSION"); err == nil && baselineCommit != "" {
		if patchAhead, err := runGitTrimmed(repoRoot, "rev-list", "--count", baselineCommit+"..HEAD"); err == nil && regexp.MustCompile(`^\d+$`).MatchString(patchAhead) {
			patch, _ = strconv.Atoi(patchAhead)
			patch++
		}
	}

	version := fmt.Sprintf("%s.%d", versionPrefix, patch)
	versionParts := strings.Split(version, ".")
	if len(versionParts) != 3 {
		return fmt.Errorf("expected semantic version major.minor.patch, got %s", version)
	}
	verMajor, _ := strconv.Atoi(versionParts[0])
	verMinor, _ := strconv.Atoi(versionParts[1])
	verPatch, _ := strconv.Atoi(versionParts[2])

	commit := "unknown"
	if hash, err := runGitTrimmed(repoRoot, "rev-parse", "--short=12", "HEAD"); err == nil && hash != "" {
		commit = hash
	}

	goEnv := append(os.Environ(), "GOCACHE="+filepath.Join(repoRoot, ".gocache"))
	ldflags := strings.Join([]string{
		"-X", "specreport/internal/version.semanticVersion=" + version,
		"-X", "specreport/internal/version.commitHash=" + commit,
	}, " ")

	targets := []buildTarget{
		{
			PackagePath:     "./cmd/hwreport",
			OutputName:      filepath.Join(repoRoot, "hwreport.exe"),
			Description:     "Hardware inventory collector",
			InternalName:    "hwreport",
			OriginalName:    "hwreport.exe",
			VersionInfoJSON: filepath.Join(repoRoot, "cmd", "hwreport", "zz_versioninfo.json"),
			VersionInfoSyso: filepath.Join(repoRoot, "cmd", "hwreport", "zz_versioninfo.syso"),
		},
		{
			PackagePath:     "./cmd/hwoverview",
			OutputName:      filepath.Join(repoRoot, "hwoverview.exe"),
			Description:     "Hardware overview report generator",
			InternalName:    "hwoverview",
			OriginalName:    "hwoverview.exe",
			VersionInfoJSON: filepath.Join(repoRoot, "cmd", "hwoverview", "zz_versioninfo.json"),
			VersionInfoSyso: filepath.Join(repoRoot, "cmd", "hwoverview", "zz_versioninfo.syso"),
		},
	}

	for _, target := range targets {
		if err := writeVersionResourceJSON(target.VersionInfoJSON); err != nil {
			return err
		}
	}
	defer cleanupVersionArtifacts(targets)

	for _, target := range targets {
		goversioninfoArgs := []string{
			"tool", "github.com/josephspurrier/goversioninfo/cmd/goversioninfo",
			"-64",
			"-o", target.VersionInfoSyso,
			"-company", "Trustfall AB",
			"-product-name", "hwreport",
			"-copyright", "Copyright (c) Trustfall AB",
			"-description", target.Description,
			"-internal-name", target.InternalName,
			"-original-name", target.OriginalName,
			"-file-version", version,
			"-product-version", version,
			"-ver-major", strconv.Itoa(verMajor),
			"-ver-minor", strconv.Itoa(verMinor),
			"-ver-patch", strconv.Itoa(verPatch),
			"-ver-build", "0",
			"-product-ver-major", strconv.Itoa(verMajor),
			"-product-ver-minor", strconv.Itoa(verMinor),
			"-product-ver-patch", strconv.Itoa(verPatch),
			"-product-ver-build", "0",
			target.VersionInfoJSON,
		}
		if err := runCommand(repoRoot, goEnv, "go", goversioninfoArgs...); err != nil {
			return fmt.Errorf("goversioninfo failed for %s: %w", target.OriginalName, err)
		}

		if err := runCommand(repoRoot, goEnv, "go", "build", "-trimpath", "-ldflags", ldflags, "-o", target.OutputName, target.PackagePath); err != nil {
			return fmt.Errorf("build %s: %w", target.OriginalName, err)
		}
		fmt.Printf("Built %s\n", target.OutputName)
	}

	return nil
}

func writeVersionResourceJSON(path string) error {
	data := versionInfoFile{}
	data.FixedFileInfo.FileFlagsMask = "3f"
	data.FixedFileInfo.FileFlags = "00"
	data.FixedFileInfo.FileOS = "040004"
	data.FixedFileInfo.FileType = "01"
	data.FixedFileInfo.FileSubType = "00"
	data.VarFileInfo.Translation.LangID = "0409"
	data.VarFileInfo.Translation.CharsetID = "04B0"

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode version info json %s: %w", path, err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write version info json %s: %w", path, err)
	}
	return nil
}

func cleanupVersionArtifacts(targets []buildTarget) {
	for _, target := range targets {
		_ = os.Remove(target.VersionInfoJSON)
		_ = os.Remove(target.VersionInfoSyso)
	}
}

func runGitTrimmed(repoRoot string, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", errors.New(strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runCommand(repoRoot string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = repoRoot
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
