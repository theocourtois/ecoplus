package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxLines      = 300
	generatedMark = "// Code generated"
)

func Scan(roots []string) ([]string, error) {
	var violations []string
	for _, root := range roots {
		root = strings.TrimSuffix(root, "/...")
		root = filepath.Clean(root)
		err := fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return skipDir(d.Name())
			}
			if !strings.HasSuffix(rel, ".go") {
				return nil
			}
			found, err := CheckFile(filepath.Join(root, rel))
			violations = append(violations, found...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

func skipDir(name string) error {
	if name == "." || name == ".." {
		return nil
	}
	if strings.HasPrefix(name, ".") || name == "vendor" || name == "bin" || name == "testdata" {
		return filepath.SkipDir
	}
	return nil
}

func CheckFile(path string) ([]string, error) {
	src, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(string(src), generatedMark) {
		return nil, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var violations []string
	if lines := strings.Count(string(src), "\n"); lines > maxLines {
		violations = append(violations, fmt.Sprintf("%s: %d lines exceeds %d", path, lines, maxLines))
	}
	violations = append(violations, commentViolations(fset, file)...)
	return violations, nil
}

func commentViolations(fset *token.FileSet, file *ast.File) []string {
	var violations []string
	for _, group := range file.Comments {
		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "//go:") {
				continue
			}
			violations = append(violations, fmt.Sprintf("%s: comment not allowed", fset.Position(c.Pos())))
		}
	}
	return violations
}
