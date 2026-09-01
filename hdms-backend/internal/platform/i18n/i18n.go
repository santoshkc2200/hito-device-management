// Package i18n localizes text the backend emits where no client exists to do
// it — CLI output today, notification email when 6.2 is built.
//
// It is deliberately unavailable to the API layer. An API response carries a
// machine-readable code and the client renders the words; see the depguard
// rule in .golangci.yml, which enforces exactly that.
package i18n

import (
	"os"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var defaultTag = language.Japanese

var matcher = language.NewMatcher([]language.Tag{language.Japanese, language.English})

func init() {
	must(message.SetString(language.Japanese, "cli.import.done", "%d 件の機器を取り込みました"))
	must(message.SetString(language.English, "cli.import.done", "Imported %d devices"))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type Catalogue struct {
	printer *message.Printer
}

func New(tag language.Tag) *Catalogue {
	return &Catalogue{printer: message.NewPrinter(tag)}
}

// FromEnv resolves the locale from LANG, defaulting to Japanese.
func FromEnv() *Catalogue {
	lang := os.Getenv("LANG")
	if lang == "" {
		return New(defaultTag)
	}
	tag, _ := language.MatchStrings(matcher, strings.SplitN(lang, ".", 2)[0])
	return New(tag)
}

func (c *Catalogue) T(key string, args ...any) string {
	return c.printer.Sprintf(key, args...)
}
