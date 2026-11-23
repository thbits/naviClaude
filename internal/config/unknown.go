package config

import (
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadWithWarnings is Load plus the unknown keys found in the file, e.g.
// "unknown key keys.detial"; unknown keys never fail the load.
func LoadWithWarnings(path string) (Config, []string, error) {
	cfg, err := Load(path)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, UnknownKeys(path), nil
}

// UnknownKeys lists keys in the config file at path (DefaultPath() if empty)
// that match no Config field, as "unknown key <dotted.path>".
func UnknownKeys(path string) []string {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	var out []string
	walkUnknown(doc.Content[0], reflect.TypeOf(Config{}), "", &out)
	return out
}

// walkUnknown checks n's keys against t's yaml tags, read by reflection so new
// fields are known without a hand-kept list.
func walkUnknown(n *yaml.Node, t reflect.Type, prefix string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	if t.Kind() != reflect.Struct || n.Kind != yaml.MappingNode {
		return
	}
	fields := yamlFields(t)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if key == "<<" {
			walkUnknown(n.Content[i+1], t, prefix, out)
			continue
		}
		ft, ok := fields[key]
		if !ok {
			*out = append(*out, "unknown key "+prefix+key)
			continue
		}
		walkUnknown(n.Content[i+1], ft, prefix+key+".", out)
	}
}

// yamlFields maps each YAML key of struct t to its field type, flattening
// ",inline" fields the way yaml.v3 does.
func yamlFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if strings.Contains(","+opts+",", ",inline,") {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range yamlFields(ft) {
					fields[k] = v
				}
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = f.Type
	}
	return fields
}
