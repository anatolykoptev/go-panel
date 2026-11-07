package resource

import (
	"fmt"

	"github.com/anatolykoptev/go-panel/shell"
)

// offeredSkins is the control's list: the panel default (resolved, so an
// unknown Config.Skin shows as graphite, which is what it renders) followed by
// the requested skins, each once, in order. It is nil when no skin is
// requested, and that nil is what switches the control off. An unregistered
// name panics: this runs once at startup and a typo is one rename.
func offeredSkins(def string, names []string) []shell.Skin {
	if len(names) == 0 {
		return nil
	}
	for _, n := range names {
		if _, ok := shell.LookupSkin(n); !ok {
			panic(fmt.Sprintf("resource: Config.Skins names %q, which is not a registered skin", n))
		}
	}
	first := shell.ChromeState{Skin: def}.SkinName()
	return shell.SkinsNamed(append([]string{first}, names...))
}
