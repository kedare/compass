package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGcpProjectsCommand(t *testing.T) {
	require.NotNil(t, gcpProjectsCmd)
	require.Equal(t, "projects", gcpProjectsCmd.Use)
	require.NotEmpty(t, gcpProjectsCmd.Short)
	require.NotEmpty(t, gcpProjectsCmd.Long)
}

func TestGcpProjectsImportCommand(t *testing.T) {
	require.NotNil(t, gcpProjectsImportCmd)
	require.Equal(t, "import", gcpProjectsImportCmd.Use)
	require.NotEmpty(t, gcpProjectsImportCmd.Short)
	require.NotEmpty(t, gcpProjectsImportCmd.Long)
	require.NotNil(t, gcpProjectsImportCmd.Run)
}

func TestGcpProjectsImportRegexFlag(t *testing.T) {
	regexFlag := gcpProjectsImportCmd.Flags().Lookup("regex")
	require.NotNil(t, regexFlag, "regex flag should exist")
	require.Equal(t, "r", regexFlag.Shorthand, "regex flag should have shorthand 'r'")
	require.NotEmpty(t, regexFlag.Usage, "regex flag should have usage description")
}

func TestGcpProjectsCommandStructure(t *testing.T) {
	// Check projects command is under gcp
	foundProjects := false
	for _, cmd := range gcpCmd.Commands() {
		if cmd.Name() == "projects" {
			foundProjects = true
			break
		}
	}
	require.True(t, foundProjects, "projects command not found under gcp command")

	// Check import command is under projects
	foundImport := false
	for _, cmd := range gcpProjectsCmd.Commands() {
		if cmd.Name() == "import" {
			foundImport = true
			break
		}
	}
	require.True(t, foundImport, "import command not found under projects command")
}

func TestFilterProjectsByRegex(t *testing.T) {
	projects := []string{
		"prod-api",
		"prod-web",
		"prod-database",
		"staging-api",
		"staging-web",
		"dev-sandbox",
		"dev-testing",
		"shared-services",
		"my-personal-project",
	}

	tests := []struct {
		name        string
		pattern     string
		expected    []string
		expectError bool
	}{
		{
			name:     "prefix match with prod",
			pattern:  "^prod-",
			expected: []string{"prod-api", "prod-web", "prod-database"},
		},
		{
			name:     "prefix match with staging",
			pattern:  "^staging-",
			expected: []string{"staging-api", "staging-web"},
		},
		{
			name:     "prefix match with dev",
			pattern:  "^dev-",
			expected: []string{"dev-sandbox", "dev-testing"},
		},
		{
			name:     "suffix match with api",
			pattern:  "-api$",
			expected: []string{"prod-api", "staging-api"},
		},
		{
			name:     "suffix match with web",
			pattern:  "-web$",
			expected: []string{"prod-web", "staging-web"},
		},
		{
			name:     "contains match",
			pattern:  "api",
			expected: []string{"prod-api", "staging-api"},
		},
		{
			name:     "alternation pattern",
			pattern:  "prod|staging",
			expected: []string{"prod-api", "prod-web", "prod-database", "staging-api", "staging-web"},
		},
		{
			name:     "complex pattern with groups",
			pattern:  "^(prod|staging)-api$",
			expected: []string{"prod-api", "staging-api"},
		},
		{
			name:     "no matches",
			pattern:  "^nonexistent-",
			expected: nil,
		},
		{
			name:     "match all",
			pattern:  ".*",
			expected: projects,
		},
		{
			name:     "case sensitive by default",
			pattern:  "^PROD-",
			expected: nil,
		},
		{
			name:     "case insensitive with flag",
			pattern:  "(?i)^PROD-",
			expected: []string{"prod-api", "prod-web", "prod-database"},
		},
		{
			name:     "single project match",
			pattern:  "^shared-services$",
			expected: []string{"shared-services"},
		},
		{
			name:     "hyphen in pattern",
			pattern:  "my-personal",
			expected: []string{"my-personal-project"},
		},
		{
			name:        "invalid regex - unclosed bracket",
			pattern:     "[invalid",
			expectError: true,
		},
		{
			name:        "invalid regex - bad escape",
			pattern:     "\\",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filterProjectsByRegex(projects, tt.pattern)

			if tt.expectError {
				require.Error(t, err, "expected error for pattern %q", tt.pattern)
				return
			}

			require.NoError(t, err, "unexpected error for pattern %q", tt.pattern)
			require.Equal(t, tt.expected, result, "mismatch for pattern %q", tt.pattern)
		})
	}
}

func TestFilterProjectsByRegex_EmptyInputs(t *testing.T) {
	t.Run("empty projects list", func(t *testing.T) {
		result, err := filterProjectsByRegex([]string{}, ".*")
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("nil projects list", func(t *testing.T) {
		result, err := filterProjectsByRegex(nil, ".*")
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("empty pattern matches all", func(t *testing.T) {
		projects := []string{"proj-a", "proj-b"}
		result, err := filterProjectsByRegex(projects, "")
		require.NoError(t, err)
		require.Equal(t, projects, result)
	})
}

func TestFilterProjectsByRegex_SpecialCharacters(t *testing.T) {
	projects := []string{
		"project.with.dots",
		"project-with-dashes",
		"project_with_underscores",
		"project123",
		"123project",
	}

	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{
			name:     "escaped dot",
			pattern:  `\.`,
			expected: []string{"project.with.dots"},
		},
		{
			name:     "unescaped dot matches any char",
			pattern:  "project.with",
			expected: []string{"project.with.dots", "project-with-dashes", "project_with_underscores"},
		},
		{
			name:     "digit pattern",
			pattern:  `\d+`,
			expected: []string{"project123", "123project"},
		},
		{
			name:     "starts with digit",
			pattern:  `^\d`,
			expected: []string{"123project"},
		},
		{
			name:     "ends with digit",
			pattern:  `\d$`,
			expected: []string{"project123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filterProjectsByRegex(projects, tt.pattern)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGcpProjectsImportHelp(t *testing.T) {
	rootCmd.SetArgs([]string{"gcp", "projects", "import", "--help"})
	defer rootCmd.SetArgs([]string{})

	err := rootCmd.Execute()
	require.NoError(t, err)
}
