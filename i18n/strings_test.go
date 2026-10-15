package i18n_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/anatolykoptev/go-panel/i18n"
)

// WithDefaults merges by reflection over exactly two kinds. If a field of
// another kind is ever added, the merge would silently skip it and leave it
// zero in every merged value — so adding one must fail here first.
func TestStringsShape(t *testing.T) {
	typ := reflect.TypeFor[i18n.Strings]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if k := f.Type.Kind(); k != reflect.String && k != reflect.Func {
			t.Errorf("Strings.%s has kind %s; WithDefaults only merges string and func fields", f.Name, k)
		}
	}
}

// Both presets define every field: an empty field in a preset would render as
// the English default under WithDefaults, so a «Russian» admin would quietly
// mix languages.
func TestPresetsAreComplete(t *testing.T) {
	for name, s := range map[string]i18n.Strings{"Default": i18n.Default(), "Russian": i18n.Russian()} {
		v := reflect.ValueOf(s)
		for i := range v.NumField() {
			f := v.Field(i)
			empty := (f.Kind() == reflect.String && f.String() == "") || (f.Kind() == reflect.Func && f.IsNil())
			if empty {
				t.Errorf("%s: Strings.%s is unset", name, v.Type().Field(i).Name)
			}
		}
	}
}

// Russian must differ from English in every user-visible text field, so a field
// copied from the English block by mistake is caught. The few that are
// legitimately identical are named.
func TestRussianDiffersFromEnglishWherePossible(t *testing.T) {
	same := map[string]string{
		"Undo": "«Вернуть» is the historical default and is already Russian",
	}
	en, ru := reflect.ValueOf(i18n.Default()), reflect.ValueOf(i18n.Russian())
	for i := range en.NumField() {
		if en.Field(i).Kind() != reflect.String {
			continue
		}
		name := en.Type().Field(i).Name
		if en.Field(i).String() == ru.Field(i).String() {
			if _, ok := same[name]; !ok {
				t.Errorf("Strings.%s is identical in Default and Russian (%q)", name, en.Field(i).String())
			}
		}
	}
}

func TestWithDefaults(t *testing.T) {
	got := i18n.Strings{Logout: "Выйти", Total: func(n int) string { return "итого" }}.WithDefaults()
	def := i18n.Default()
	if got.Logout != "Выйти" || got.Total(3) != "итого" {
		t.Error("an explicitly set field was overwritten by its default")
	}
	if got.Settings != def.Settings || got.New != def.New || got.Deleted("x") != def.Deleted("x") {
		t.Error("an unset field did not fall back to the default")
	}
	again := got.WithDefaults()
	if again.Logout != got.Logout || again.Total(1) != got.Total(1) {
		t.Error("WithDefaults is not idempotent")
	}
}

func TestFromFallsBackToDefault(t *testing.T) {
	if got := i18n.From(context.Background()); got.Logout != i18n.Default().Logout {
		t.Errorf("a context without Strings must yield the English default, got Logout=%q", got.Logout)
	}
	ctx := i18n.With(context.Background(), i18n.Strings{Logout: "Выйти"})
	got := i18n.From(ctx)
	if got.Logout != "Выйти" || got.Settings != i18n.Default().Settings {
		t.Errorf("With must store the override merged over the defaults, got %+v", got.Logout)
	}
}
