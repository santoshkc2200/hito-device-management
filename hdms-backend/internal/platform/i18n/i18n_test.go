package i18n_test

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/i18n"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func TestCatalogueRendersPerLocale(t *testing.T) {
	t.Parallel()

	require.Equal(t, "3 件の機器を取り込みました", i18n.New(language.Japanese).T("cli.import.done", 3))
	require.Equal(t, "Imported 3 devices", i18n.New(language.English).T("cli.import.done", 3))
}

func TestUnknownKeyReturnsTheKeyRatherThanPanicking(t *testing.T) {
	t.Parallel()
	require.Equal(t, "cli.nope", i18n.New(language.Japanese).T("cli.nope"))
}

func TestFromEnvDefaultsToJapanese(t *testing.T) {
	t.Setenv("LANG", "")
	require.Equal(t, "3 件の機器を取り込みました", i18n.FromEnv().T("cli.import.done", 3))
}

func TestFromEnvHonoursAnEnglishLang(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	require.Equal(t, "Imported 3 devices", i18n.FromEnv().T("cli.import.done", 3))
}
