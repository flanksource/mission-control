package static

import "embed"

//go:embed logo.svg tailwind.min.js login.html callback_success.html
var FS embed.FS

var LoginHTML = must("login.html")
var CallbackSuccessHTML = must("callback_success.html")

func must(name string) string {
	b, err := FS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(b)
}
