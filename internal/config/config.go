// Package config loads the small declarative lean-guard repository contract.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Commands contains the one explicitly authorized repository command.
type Commands struct {
	Lint string `yaml:"lint"`
}

// ICodeRaw is the provider delivery policy. Invoking push remains the
// external-write authorization; this block cannot start delivery itself.
type ICodeRaw struct {
	AutoSubmit *bool    `yaml:"auto_submit"`
	Reviewers  []string `yaml:"reviewers"`
}

// RepoConfig is the complete lean .no-mistakes.yaml schema.
type RepoConfig struct {
	OutputLanguage string   `yaml:"output_language"`
	Commands       Commands `yaml:"commands"`
	ICode          ICodeRaw `yaml:"icode"`
}

// Language returns the configured output language with the stable English
// default used when a repository has no configuration.
func (c *RepoConfig) Language() string {
	if c != nil && c.OutputLanguage == "zh-CN" {
		return "zh-CN"
	}
	return "en"
}

// ICodeAutoSubmit makes the repository policy eligible for explicit caller
// authorization. The candidate checkout never grants submit authority alone.
func (c *RepoConfig) ICodeAutoSubmit() bool {
	return c != nil && c.ICode.AutoSubmit != nil && *c.ICode.AutoSubmit
}

// ICodeReviewers returns the canonical reviewer set used for both policy
// binding and provider writes. Order and duplicates are not policy changes.
func (c *RepoConfig) ICodeReviewers() []string {
	seen := map[string]struct{}{}
	reviewers := make([]string, 0)
	if c != nil {
		for _, reviewer := range c.ICode.Reviewers {
			reviewer = strings.TrimSpace(reviewer)
			if reviewer == "" {
				continue
			}
			if _, exists := seen[reviewer]; exists {
				continue
			}
			seen[reviewer] = struct{}{}
			reviewers = append(reviewers, reviewer)
		}
	}
	sort.Strings(reviewers)
	return reviewers
}

// ICodePolicyHash returns a stable, non-secret binding for the repository,
// branch, and complete set of repository-controlled iCode writes. Automated
// callers must echo this hash with an explicit one-invocation capability.
func (c *RepoConfig) ICodePolicyHash(repository, branch string) string {
	repository = strings.TrimSuffix(strings.Trim(strings.TrimSpace(repository), "/"), ".git")
	payload, err := json.Marshal(struct {
		Contract   string   `json:"contract"`
		Repository string   `json:"repository"`
		Branch     string   `json:"branch"`
		AutoSubmit bool     `json:"auto_submit"`
		Reviewers  []string `json:"reviewers"`
	}{
		Contract: "no-mistakes/icode-submit-policy/v1", Repository: repository,
		Branch: branch, AutoSubmit: c.ICodeAutoSubmit(), Reviewers: c.ICodeReviewers(),
	})
	if err != nil {
		panic("encode fixed iCode policy shape: " + err.Error())
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DetectOutputLanguage reads only the presentation hint. It remains usable
// when another config field is invalid so CLI blockers can still be rendered
// in the user's selected language.
func DetectOutputLanguage(dir string) string {
	payload, err := os.ReadFile(filepath.Join(dir, ".no-mistakes.yaml"))
	if err != nil {
		return "en"
	}
	var presentation struct {
		OutputLanguage string `yaml:"output_language"`
	}
	if yaml.Unmarshal(payload, &presentation) == nil && presentation.OutputLanguage == "zh-CN" {
		return "zh-CN"
	}
	return "en"
}

var reviewerPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// LoadRepo reads dir/.no-mistakes.yaml, returning zero values when absent.
func LoadRepo(dir string) (*RepoConfig, error) {
	payload, err := os.ReadFile(filepath.Join(dir, ".no-mistakes.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return &RepoConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read repo config: %w", err)
	}
	cfg := &RepoConfig{}
	decoder := yaml.NewDecoder(strings.NewReader(string(payload)))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse repo config: %w", err)
	}
	for _, reviewer := range cfg.ICode.Reviewers {
		if !reviewerPattern.MatchString(reviewer) {
			return nil, fmt.Errorf("parse repo config: invalid iCode reviewer %q", reviewer)
		}
	}
	switch cfg.OutputLanguage {
	case "", "en", "zh-CN":
	default:
		return nil, fmt.Errorf("parse repo config: output_language must be en or zh-CN")
	}
	return cfg, nil
}
