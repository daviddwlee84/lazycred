package version

import "runtime/debug"

var Build = "dev"

func Resolve(injected, module string) string {
	if injected != "" && injected != "dev" {
		return injected
	}
	if module != "" && module != "(devel)" {
		return module
	}
	return "dev"
}
func Current() string {
	module := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		module = info.Main.Version
	}
	return Resolve(Build, module)
}
