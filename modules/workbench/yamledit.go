package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Form edits of YAML files: set a few keys and keep everything else (order, comments, keys
// the form does not know) as the owner wrote it.

// setYAML sets dotted keys ("runtime.max_steps") in a YAML document. A nil value, an empty
// string or a zero number removes the key.
func setYAML(doc []byte, set map[string]any) ([]byte, error) {
	var root yaml.Node
	if len(bytes.TrimSpace(doc)) > 0 {
		if err := yaml.Unmarshal(doc, &root); err != nil {
			return nil, err
		}
	}
	if root.Kind == 0 {
		root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the document is not a mapping")
	}
	for key, v := range set {
		if err := setPath(root.Content[0], strings.Split(key, "."), v); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case float64:
		return x == 0
	case int:
		return x == 0
	case bool:
		return !x
	}
	return false
}

func setPath(m *yaml.Node, keys []string, v any) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != keys[0] {
			continue
		}
		if len(keys) == 1 {
			if empty(v) {
				m.Content = append(m.Content[:i], m.Content[i+2:]...)
				return nil
			}
			var n yaml.Node
			if err := n.Encode(v); err != nil {
				return err
			}
			n.HeadComment, n.LineComment = m.Content[i+1].HeadComment, m.Content[i+1].LineComment
			m.Content[i+1] = &n
			return nil
		}
		child := m.Content[i+1]
		if child.Kind != yaml.MappingNode {
			if empty(v) {
				return nil
			}
			*child = yaml.Node{Kind: yaml.MappingNode}
		}
		if err := setPath(child, keys[1:], v); err != nil {
			return err
		}
		if len(child.Content) == 0 {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
		}
		return nil
	}
	if empty(v) {
		return nil
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Value: keys[0]}
	if len(keys) == 1 {
		var n yaml.Node
		if err := n.Encode(v); err != nil {
			return err
		}
		m.Content = append(m.Content, k, &n)
		return nil
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	if err := setPath(child, keys[1:], v); err != nil {
		return err
	}
	m.Content = append(m.Content, k, child)
	return nil
}

// setFront sets keys in a Markdown file's YAML front matter, adding front matter when there
// is none; the body is kept as it is.
func setFront(file []byte, set map[string]any) ([]byte, error) {
	head, body, err := splitFront(file)
	if err != nil {
		return nil, err
	}
	out, err := setYAML(head, set)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(out) + "---\n" + body), nil
}

// setBody replaces a Markdown file's body, keeping its front matter.
func setBody(file []byte, body string) ([]byte, error) {
	head, _, err := splitFront(file)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return []byte(body), nil
	}
	return []byte("---\n" + string(head) + "---\n" + body), nil
}
