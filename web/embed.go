// Package web contains the static web UI.
package web

import "embed"

//go:embed index.html app.css datenschutz.html nutzungsbedingungen.html js
var FS embed.FS
