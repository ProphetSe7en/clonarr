package main

import "strings"

// profileStore manages imported profiles as individual JSON files in a directory.
// Delegates all CRUD to the embedded FileStore.
type profileStore struct {
	*FileStore[ImportedProfile]
}

func newProfileStore(dir string) *profileStore {
	return &profileStore{NewFileStore[ImportedProfile](dir)}
}

// Add saves one or more profiles, skipping duplicates (same Name + AppType).
func (ps *profileStore) Add(profiles []ImportedProfile) error {
	_, err := ps.AddNew(profiles, nil)
	return err
}

// sanitizeFilename creates a safe filename from a profile name.
func sanitizeFilename(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		if r == ' ' || r == '/' || r == '\\' {
			return '-'
		}
		return -1
	}, name)
	// Collapse multiple dashes
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if name == "" {
		name = "profile"
	}
	return name
}
