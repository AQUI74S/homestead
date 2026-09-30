// Package web contains the static web UI.
package web

import "embed"

//go:embed index.html app.js hv.js app.css datenschutz.html nutzungsbedingungen.html
var FS embed.FS
