// Package fonts incrusta la fuente monoespaciada usada por el motor de render de
// tickets. Los bytes se sirven same-origin desde el servidor loopback de
// internal/htmlrender (ver server.go), no desde aquí.
package fonts

import _ "embed"

//go:embed CaskaydiaCoveNerdFontMono-Regular.ttf
var CaskaydiaCoveTTF []byte
