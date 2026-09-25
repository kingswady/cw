// Command cw reads your apps, servers, installers, backups and runs from the
// platform's customer API (/api/v1) with a token minted in the dashboard.
package main

import "os"

// version is set at build time: -ldflags "-X main.version=v1.0.0".
var version = "dev"

func main() {
	os.Exit(newApp().run(os.Args[1:]))
}
