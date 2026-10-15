package resource

import "github.com/anatolykoptev/go-panel/i18n"

// Strings is the wording of the panel chrome; see Config.Strings. It is an
// alias of i18n.Strings because shell and auth render text too and cannot
// import this package.
type Strings = i18n.Strings

// DefaultStrings returns the English strings the panel renders when
// Config.Strings is left unset.
func DefaultStrings() Strings { return i18n.Default() }

// RussianStrings returns a ready Russian preset for Config.Strings. Override
// single fields on the returned value to adjust the wording:
//
//	s := resource.RussianStrings()
//	s.New = "+ Добавить"
//	resource.New(resource.Config{Strings: s, ...})
func RussianStrings() Strings { return i18n.Russian() }
