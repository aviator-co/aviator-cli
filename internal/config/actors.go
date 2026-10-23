//go:build experimental

package config

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"emperror.dev/errors"
	"go.yaml.in/yaml/v3"
)

// LocalActor is one entry in the local actors file. The file is maintained
// apart from the remote actors and the server reconciles the two; the CLI
// never sends it to the API.
type LocalActor struct {
	Name        string `yaml:"name"`
	Role        string `yaml:"role"`
	Description string `yaml:"description"`
}

// ActorsFile returns the path of the local actors file:
// $XDG_CONFIG_HOME/aviator/actors.yaml, else ~/.config/aviator/actors.yaml.
func ActorsFile() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "aviator", "actors.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to find the home directory")
	}
	return filepath.Join(home, ".config", "aviator", "actors.yaml"), nil
}

// LoadActors reads the actors file at path. A missing or empty file has no
// actors. Unknown keys are an error, so a misspelled or capitalized key
// (`Name:`) is reported instead of silently read as blank.
func LoadActors(path string) ([]LocalActor, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s", path)
	}
	var file struct {
		Actors []LocalActor `yaml:"actors"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.Wrapf(err, "failed to parse %s", path)
	}
	return file.Actors, nil
}

// EditActor adds the actor called name to the actors file at path, or
// overrides the one already there. A nil role or description leaves the
// current value alone; a new actor needs a role. The file is edited in place,
// so its comments and ordering survive.
func EditActor(path, name string, role, description *string) (actor LocalActor, created bool, err error) {
	// Strict-load first so a file the CLI couldn't read back is never rewritten.
	if _, err := LoadActors(path); err != nil {
		return LocalActor{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return LocalActor{}, false, errors.Wrapf(err, "failed to read %s", path)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return LocalActor{}, false, errors.Wrapf(err, "failed to parse %s", path)
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return LocalActor{}, false, errors.Errorf("%s must be a mapping with an actors list", path)
	}

	list := mappingValue(root, "actors")
	switch {
	case list == nil:
		list = &yaml.Node{Kind: yaml.SequenceNode}
		root.Content = append(root.Content, strNode("actors"), list)
	case list.Kind != yaml.SequenceNode:
		// A bare `actors:` key is null; LoadActors rejects anything else.
		*list = yaml.Node{Kind: yaml.SequenceNode}
	}

	var entry *yaml.Node
	for _, item := range list.Content {
		if v := mappingValue(item, "name"); v != nil && v.Value == name {
			entry = item
			break
		}
	}
	if entry == nil {
		if role == nil {
			return LocalActor{}, false, errors.Errorf("actor %q is new, so it needs a role", name)
		}
		entry = &yaml.Node{Kind: yaml.MappingNode}
		setMappingValue(entry, "name", name)
		list.Content = append(list.Content, entry)
		created = true
	}
	if role != nil {
		setMappingValue(entry, "role", *role)
	}
	if description != nil {
		setMappingValue(entry, "description", *description)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return LocalActor{}, false, errors.Wrap(err, "failed to encode the actors file")
	}
	if err := enc.Close(); err != nil {
		return LocalActor{}, false, errors.Wrap(err, "failed to encode the actors file")
	}
	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return LocalActor{}, false, err
	}
	if err := entry.Decode(&actor); err != nil {
		return LocalActor{}, false, errors.Wrap(err, "failed to read back the edited actor")
	}
	return actor, created, nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(m *yaml.Node, key, value string) {
	if v := mappingValue(m, key); v != nil {
		*v = *strNode(value)
		return
	}
	m.Content = append(m.Content, strNode(key), strNode(value))
}

// strNode tags the scalar as a string so a value like "123" or "yes" is
// quoted rather than read back as another type.
func strNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// writeFileAtomic replaces path through a temp file in the same directory, so
// an interrupted write never leaves a half-written actors file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return errors.Wrapf(err, "failed to create %s", dir)
	}
	tmp, err := os.CreateTemp(dir, ".actors-*.yaml")
	if err != nil {
		return errors.Wrapf(err, "failed to write %s", path)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errors.Wrapf(err, "failed to write %s", path)
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrapf(err, "failed to write %s", path)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Wrapf(err, "failed to write %s", path)
	}
	return nil
}
