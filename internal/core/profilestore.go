package core

// ProfileStore manages imported profiles as individual JSON files in a directory.
// It embeds FileStore for generic CRUD and adds profile-specific behavior.
type ProfileStore struct {
	*FileStore[ImportedProfile, *ImportedProfile]
}

func NewProfileStore(dir string) *ProfileStore {
	return &ProfileStore{
		FileStore: NewFileStore[ImportedProfile, *ImportedProfile](dir),
	}
}
