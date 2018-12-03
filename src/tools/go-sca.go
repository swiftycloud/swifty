/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"go/token"
	"go/parser"
	"go/ast"
	"os"
)

func checkFileHasType(fname, typ string) bool {
	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, fname, nil, 0)
	if err != nil {
		return false
	}

	for _, d := range f.Decls {
		x, ok := d.(*ast.GenDecl)
		if ok && x.Tok == token.TYPE && len(x.Specs) > 0 {
			s, ok := x.Specs[0].(*ast.TypeSpec)
			if ok && s.Name != nil && s.Name.Name == typ {
				return true
			}
		}
	}

	return false
}

func main() {
	switch os.Args[1] {
	case "-type":
		res := checkFileHasType(os.Args[2], os.Args[3])
		if !res {
			os.Exit(1)
		}
	default:
		os.Exit(2)
	}
}
