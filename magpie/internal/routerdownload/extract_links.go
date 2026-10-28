package routerdownload

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type archiveLink struct{ name, target string }

// Record only archive names. Never create or follow a filesystem symlink.
// ZIP links and tar hard links/special entries retain their rejection policy.
func (e *extractor) deferLink(name, target string) error {
	clean, err := e.claim(name)
	if err != nil {
		return err
	}
	if target == "" || len(target) > 4096 || strings.HasPrefix(target, "/") {
		return fmt.Errorf("unsafe archive link target: %q", target)
	}
	// Relative . and .. are permitted only while they stay inside the archive.
	// Validate other components before cleaning, including Windows aliases.
	parent := path.Dir(clean)
	for _, component := range strings.Split(target, "/") {
		switch component {
		case ".":
			continue
		case "..":
			if parent == "." {
				return errors.New("archive link target escapes extraction directory")
			}
			parent = path.Dir(parent)
		default:
			if _, err := safeEntry(component); err != nil {
				return err
			}
			parent = path.Join(parent, component)
		}
	}
	resolved, err := safeEntry(parent)
	if err != nil {
		return err
	}
	e.links = append(e.links, archiveLink{clean, resolved})
	return nil
}

// Resolve chains against the complete archive namespace, then copy each alias
// from its final regular file. Directory links and dangling/cyclic links fail
// closed. Copies use the same byte budget, hashing and flush path as originals.
func (e *extractor) materializeLinks() error {
	files := make(map[string]extractedFile, len(e.receipt.Files))
	for _, f := range e.receipt.Files {
		files[f.Path] = f
	}
	links := make(map[string]string, len(e.links))
	for _, link := range e.links {
		links[link.name] = link.target
	}
	resolved := make(map[string]extractedFile, len(e.links))
	for _, link := range e.links {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		current := link.name
		trail := make(map[string]bool)
		var source extractedFile
		for {
			if f, ok := files[current]; ok {
				source = f
				break
			}
			if f, ok := resolved[current]; ok {
				source = f
				break
			}
			if trail[current] {
				return fmt.Errorf("cyclic archive link: %s", link.name)
			}
			trail[current] = true
			next, ok := links[current]
			if !ok {
				return fmt.Errorf("archive link target is not a regular file: %s", link.name)
			}
			current = next
		}
		for name := range trail {
			resolved[name] = source
		}
		if err := e.copyLink(link.name, source); err != nil {
			return err
		}
	}
	return nil
}

func (e *extractor) copyLink(name string, source extractedFile) error {
	f, err := os.Open(filepath.Join(e.root, filepath.FromSlash(source.Path)))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != source.Size {
		return errors.New("archive link target changed")
	}
	return e.writeEntry(name, source.Size, info.Mode(), f)
}
