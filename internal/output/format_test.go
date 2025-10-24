package output

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultFormatAndJSONMode(t *testing.T) {
	t.Run("default_format_with_empty_env", func(t *testing.T) {
		t.Setenv("COMPASS_OUTPUT", "")
		format := DefaultFormat("text", []string{"text", "json"})
		require.Equal(t, "text", format)
		require.False(t, IsJSONMode())
	})

	t.Run("default_format_with_json_env", func(t *testing.T) {
		t.Setenv("COMPASS_OUTPUT", "json")
		format := DefaultFormat("text", []string{"text", "json"})
		require.Equal(t, "json", format)
		require.True(t, IsJSONMode())
	})

	t.Run("default_format_with_unsupported_env", func(t *testing.T) {
		t.Setenv("COMPASS_OUTPUT", "unsupported")
		format := DefaultFormat("detailed", []string{"text", "json", "detailed"})
		require.Equal(t, "detailed", format)
		require.False(t, IsJSONMode())
	})

	t.Run("set_format_to_json", func(t *testing.T) {
		t.Setenv("COMPASS_OUTPUT", "")
		SetFormat("json")
		require.True(t, IsJSONMode())
	})

	t.Run("set_format_to_text", func(t *testing.T) {
		t.Setenv("COMPASS_OUTPUT", "json")
		SetFormat("text")
		require.False(t, IsJSONMode())
	})
}
