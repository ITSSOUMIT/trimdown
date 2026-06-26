// Package allfilters blank-imports every filter package so their init()
// registrations run. cmd/trimdown imports it for side effects. Adding a new
// filter package = one import line here.
package allfilters

import (
	_ "github.com/itssoumit/trimdown/internal/declarative"
	_ "github.com/itssoumit/trimdown/internal/filters/cloud"
	_ "github.com/itssoumit/trimdown/internal/filters/git"
	_ "github.com/itssoumit/trimdown/internal/filters/golang"
	_ "github.com/itssoumit/trimdown/internal/filters/js"
	_ "github.com/itssoumit/trimdown/internal/filters/python"
	_ "github.com/itssoumit/trimdown/internal/filters/ruby"
	_ "github.com/itssoumit/trimdown/internal/filters/sys"
	_ "github.com/itssoumit/trimdown/internal/filters/vcshost"
)
