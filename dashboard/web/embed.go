package web

import "embed"

//go:embed templates/*.html templates/fragments/*.html
var Templates embed.FS

//go:embed static/*
var Static embed.FS
