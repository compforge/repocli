package syntax

import (
	"path"
	"strconv"
	"strings"

	gs "github.com/odvcencio/gotreesitter"
)

func pythonString(n *gs.Node, lang *gs.Language, source []byte) (string, bool) {
	if n == nil || n.Type(lang) != "string" {
		return "", false
	}
	raw := n.Text(source)
	if len(raw) < 2 || (raw[0] != '\'' && raw[0] != '"') || raw[len(raw)-1] != raw[0] || strings.ContainsAny(raw[1:len(raw)-1], "\\\n\r") || strings.Contains(raw[1:len(raw)-1], string(raw[0])) {
		return "", false
	}
	return raw[1 : len(raw)-1], true
}

func pythonCall(f *Facts, name string, n *gs.Node, lang *gs.Language, source []byte) {
	fn := n.ChildByFieldName("function", lang)
	if fn == nil {
		return
	}
	callee := fn.Text(source)
	args := n.ChildByFieldName("arguments", lang)
	if callee == "__import__" || strings.HasSuffix(callee, ".import_module") {
		if (callee == "__import__" || callee == "importlib.import_module") && args != nil && args.NamedChildCount() == 1 {
			if target, ok := pythonString(args.NamedChild(0), lang, source); ok && target != "" && !strings.HasPrefix(target, ".") {
				f.Imports = append(f.Imports, Import{Path: target})
				return
			}
		}
	} else if callee == "sys.path.insert" || callee == "sys.path.append" {
		count := 1
		if callee == "sys.path.insert" {
			count = 2
		}
		if args != nil && args.NamedChildCount() == count {
			indexOK := count == 1
			if count == 2 {
				_, err := strconv.Atoi(args.NamedChild(0).Text(source))
				indexOK = err == nil
			}
			if root, ok := pythonPath(name, args.NamedChild(count-1), lang, source); ok && indexOK {
				f.SearchPaths = append(f.SearchPaths, root)
				return
			}
		}
	} else if callee != "exec" && callee != "eval" && !strings.HasPrefix(callee, "sys.path.") {
		return
	}
	f.Issues = append(f.Issues, "runtime dependency discovery: "+callee)
}

// Only file-relative pathlib expressions are evaluated. Bare strings depend on
// cwd; variables, external roots and executable expressions remain diagnostics.
func pythonPath(name string, n *gs.Node, lang *gs.Language, source []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.Type(lang) {
	case "identifier":
		if n.Text(source) == "__file__" {
			return name, true
		}
	case "call":
		fn := n.ChildByFieldName("function", lang)
		args := n.ChildByFieldName("arguments", lang)
		if fn == nil || args == nil {
			return "", false
		}
		if (fn.Text(source) == "str" || fn.Text(source) == "Path" || fn.Text(source) == "pathlib.Path") && args.NamedChildCount() == 1 {
			return pythonPath(name, args.NamedChild(0), lang, source)
		}
		if fn.Type(lang) == "attribute" && args.NamedChildCount() == 0 {
			attr := fn.ChildByFieldName("attribute", lang)
			if attr != nil && attr.Text(source) == "resolve" {
				return pythonPath(name, fn.ChildByFieldName("object", lang), lang, source)
			}
		}
	case "attribute":
		attr := n.ChildByFieldName("attribute", lang)
		if attr != nil && attr.Text(source) == "parent" {
			root, ok := pythonPath(name, n.ChildByFieldName("object", lang), lang, source)
			if ok && root != "." {
				return path.Dir(root), true
			}
		}
	case "subscript":
		value := n.ChildByFieldName("value", lang)
		index := n.ChildByFieldName("subscript", lang)
		if value != nil && index != nil && value.Type(lang) == "attribute" {
			attr := value.ChildByFieldName("attribute", lang)
			if attr != nil && attr.Text(source) == "parents" {
				root, ok := pythonPath(name, value.ChildByFieldName("object", lang), lang, source)
				depth, err := strconv.Atoi(index.Text(source))
				if ok && err == nil && depth >= 0 && depth < 100 {
					for i := 0; i <= depth; i++ {
						if root == "." {
							return "", false
						}
						root = path.Dir(root)
					}
					return root, true
				}
			}
		}
	case "binary_operator":
		left := n.ChildByFieldName("left", lang)
		right := n.ChildByFieldName("right", lang)
		operator := n.ChildByFieldName("operator", lang)
		if operator != nil && operator.Text(source) == "/" {
			root, ok := pythonPath(name, left, lang, source)
			suffix, literal := pythonString(right, lang, source)
			if ok && literal && !path.IsAbs(suffix) {
				joined := path.Join(root, suffix)
				if joined != ".." && !strings.HasPrefix(joined, "../") {
					return joined, true
				}
			}
		}
	}
	return "", false
}
