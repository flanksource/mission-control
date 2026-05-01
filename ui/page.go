package ui

// pageHTML returns the standalone HTML shell that boots the React app.
//
// The bundle is an IIFE produced by vite in lib mode (see ui/frontend/vite.config.ts)
// and is inlined so no separate asset route is needed.
func pageHTML() string {
	return `<!doctype html>
<html lang="en" data-theme="light" data-density="comfortable">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Mission Control</title>
    <link rel="icon" type="image/svg+xml" href="/ui/favicon.svg" />
    <script src="https://code.iconify.design/iconify-icon/2.1.0/iconify-icon.min.js"></script>
    <style>iconify-icon { display: inline-block; width: 1em; height: 1em; vertical-align: -0.125em; }</style>
    <style>` + bundleCSS + `</style>
  </head>
  <body class="bg-background text-foreground antialiased">
    <div id="root"></div>
    <script>` + bundleJS + `</script>
  </body>
</html>`
}
