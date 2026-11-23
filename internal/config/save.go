package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"gopkg.in/yaml.v3"
)

// mergeIntoFile renders cfg onto the YAML already at path, touching only the
// keys whose effective value changed, so comments, order and unknown keys survive.
func mergeIntoFile(cfg Config, path string) ([]byte, error) {
	return mergeIntoFileSince(cfg, nil, path)
}

// mergeIntoFileSince is mergeIntoFile, but with prev set it writes only the
// fields cfg changed since prev, so edits made to the file meanwhile survive.
func mergeIntoFileSince(cfg Config, prev *Config, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: read: %w", err)
	}

	var doc yaml.Node
	base := DefaultConfig()
	if len(bytes.TrimSpace(data)) > 0 {
		// Both decodes must succeed: a file Load rejects would otherwise be
		// replaced by defaults plus the one changed value.
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("config: refusing to overwrite %s, it does not parse: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &base); err != nil {
			return nil, fmt.Errorf("config: refusing to overwrite %s, it does not parse: %w", path, err)
		}
		sanitizeConfig(&base)
	}

	// yaml.v3 drops a comment-only document, so its text is carried over by hand.
	var lead []byte
	if len(doc.Content) == 0 && len(bytes.TrimSpace(data)) > 0 {
		lead = append(bytes.TrimRight(data, "\n"), '\n')
	}
	root, err := rootMapping(&doc)
	if err != nil {
		return nil, fmt.Errorf("config: refusing to overwrite %s: %w", path, err)
	}

	var want, have yaml.Node
	if err := want.Encode(cfg); err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	if prev != nil {
		base = *prev
	}
	if err := have.Encode(base); err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	mergeMapping(root, &want, &have)

	buf := bytes.NewBuffer(lead)
	enc := yaml.NewEncoder(buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("config: marshal: %w", err)
	}
	return buf.Bytes(), nil
}

// rootMapping returns doc's top-level mapping, creating it for an empty or
// comment-only file.
func rootMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, errors.New("unexpected YAML document")
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("top level is not a mapping")
	}
	return root, nil
}

// mergeMapping writes into dst each key of want whose value differs from have
// (the effective config on disk), recursing into nested mappings.
func mergeMapping(dst, want, have *yaml.Node) {
	for i := 0; i+1 < len(want.Content); i += 2 {
		key, val := want.Content[i], want.Content[i+1]
		old := mappingValue(have, key.Value)
		di := mappingIndex(dst, key.Value)

		if val.Kind == yaml.MappingNode {
			if di >= 0 && dst.Content[di+1].Kind == yaml.MappingNode {
				mergeMapping(dst.Content[di+1], val, old)
				continue
			}
			sub := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mergeMapping(sub, val, old)
			if di >= 0 {
				// A null or scalar where a section belongs: replace it outright.
				if len(sub.Content) > 0 || dst.Content[di+1].Tag != "!!null" {
					replaceValue(dst, di, sub)
				}
			} else if len(sub.Content) > 0 {
				dst.Content = append(dst.Content, plainKey(key), sub)
			}
			continue
		}

		if old != nil && sameValue(val, old) {
			continue
		}
		if di >= 0 {
			replaceValue(dst, di, val)
		} else {
			dst.Content = append(dst.Content, plainKey(key), val)
		}
	}
}

// replaceValue swaps the value of the key at di, keeping the old value's comments.
func replaceValue(m *yaml.Node, di int, val *yaml.Node) {
	old := m.Content[di+1]
	val.HeadComment, val.LineComment, val.FootComment = old.HeadComment, old.LineComment, old.FootComment
	m.Content[di+1] = val
}

func plainKey(k *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.Value}
}

// mappingIndex returns the index of key's key node in m, or -1.
func mappingIndex(m *yaml.Node, key string) int {
	if m == nil || m.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if i := mappingIndex(m, key); i >= 0 {
		return m.Content[i+1]
	}
	return nil
}

// sameValue compares two nodes by their decoded values, ignoring style.
func sameValue(a, b *yaml.Node) bool {
	var va, vb any
	if a.Decode(&va) != nil || b.Decode(&vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// writeFileAtomic replaces path via a synced temp file and rename, so a crash
// leaves either the old file or the new one, never a truncated mix.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	// Write through a symlink (e.g. a dotfiles repo) instead of replacing it.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	// Persist the rename itself; best effort, as not every filesystem allows it.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
