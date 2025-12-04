package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

// SharedFolder represents a configured shared directory.
type SharedFolder struct {
	LocalPath   string `json:"local_path"`   // Absolute path on disk
	VirtualPath string `json:"virtual_path"` // Path shown to peers
}

// SharedFile represents an indexed file available for sharing.
type SharedFile struct {
	LocalPath    string               `json:"local_path"`    // Absolute path on disk
	SoulseekPath string               `json:"soulseek_path"` // Path as seen by peers (backslash-separated)
	Size         int64                `json:"size"`          // File size in bytes
	Extension    string               `json:"extension"`     // File extension without dot (e.g., "mp3")
	Attributes   []peer.FileAttribute `json:"attributes"`    // Audio metadata (optional)
}

// FileSharer manages shared folders and file indexing.
type FileSharer struct {
	mu           sync.RWMutex
	folders      []SharedFolder
	files        map[string]*SharedFile // keyed by SoulseekPath
	attrResolver func(string) []peer.FileAttribute
}

// persistedIndex is the JSON format for saved indexes.
type persistedIndex struct {
	Version int            `json:"version"`
	Folders []SharedFolder `json:"folders"`
	Files   []*SharedFile  `json:"files"`
	SavedAt time.Time      `json:"saved_at"`
}

const currentIndexVersion = 1

// NewFileSharer creates a new file sharer.
func NewFileSharer() *FileSharer {
	return &FileSharer{
		files: make(map[string]*SharedFile),
	}
}

// AddSharedFolder registers a folder to share.
// localPath must be an absolute path to an existing directory.
// virtualPath is the name shown to peers (no slashes allowed).
func (fs *FileSharer) AddSharedFolder(localPath, virtualPath string) error {
	if !filepath.IsAbs(localPath) {
		return errors.New("localPath must be absolute")
	}

	if strings.ContainsAny(virtualPath, `/\`) {
		return errors.New("virtualPath cannot contain slashes")
	}

	if virtualPath == "" {
		return errors.New("virtualPath cannot be empty")
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat path: %w", err)
	}

	if !info.IsDir() {
		return errors.New("localPath must be a directory")
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check for duplicate virtual path
	for _, f := range fs.folders {
		if f.VirtualPath == virtualPath {
			return fmt.Errorf("virtualPath %q already registered", virtualPath)
		}
	}

	fs.folders = append(fs.folders, SharedFolder{
		LocalPath:   localPath,
		VirtualPath: virtualPath,
	})

	return nil
}

// RemoveSharedFolder unregisters a folder and removes its files from the index.
func (fs *FileSharer) RemoveSharedFolder(virtualPath string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Find and remove the folder
	idx := -1
	for i, f := range fs.folders {
		if f.VirtualPath == virtualPath {
			idx = i
			break
		}
	}

	if idx == -1 {
		return
	}

	fs.folders = append(fs.folders[:idx], fs.folders[idx+1:]...)

	// Remove files with this virtual path prefix
	prefix := virtualPath + `\`
	for path := range fs.files {
		if strings.HasPrefix(path, prefix) || path == virtualPath {
			delete(fs.files, path)
		}
	}
}

// SetAttributeResolver sets a callback for extracting audio metadata.
// Called during Rescan() for each file. If nil, no attributes are extracted.
func (fs *FileSharer) SetAttributeResolver(fn func(localPath string) []peer.FileAttribute) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.attrResolver = fn
}

// Rescan rebuilds the file index by walking all shared folders.
// Clears existing index and rebuilds from scratch.
func (fs *FileSharer) Rescan() error {
	fs.mu.Lock()
	folders := make([]SharedFolder, len(fs.folders))
	copy(folders, fs.folders)
	attrResolver := fs.attrResolver
	fs.mu.Unlock()

	newFiles := make(map[string]*SharedFile)

	for _, folder := range folders {
		err := filepath.WalkDir(folder.LocalPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip hidden files and directories (starting with .)
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip directories (but continue walking into them)
			if d.IsDir() {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return err
			}

			// Build Soulseek path
			soulseekPath := localToSoulseek(path, folder.LocalPath, folder.VirtualPath)

			// Extract extension
			ext := strings.TrimPrefix(filepath.Ext(d.Name()), ".")
			ext = strings.ToLower(ext)

			file := &SharedFile{
				LocalPath:    path,
				SoulseekPath: soulseekPath,
				Size:         info.Size(),
				Extension:    ext,
			}

			// Call attribute resolver if set
			if attrResolver != nil {
				file.Attributes = attrResolver(path)
			}

			newFiles[soulseekPath] = file
			return nil
		})

		if err != nil {
			return fmt.Errorf("walk %s: %w", folder.LocalPath, err)
		}
	}

	fs.mu.Lock()
	fs.files = newFiles
	fs.mu.Unlock()

	return nil
}

// GetFile looks up a file by its Soulseek path.
// Returns nil if not found.
func (fs *FileSharer) GetFile(soulseekPath string) *SharedFile {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.files[soulseekPath]
}

// GetSharedFiles returns all indexed files.
func (fs *FileSharer) GetSharedFiles() []*SharedFile {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files := make([]*SharedFile, 0, len(fs.files))
	for _, f := range fs.files {
		files = append(files, f)
	}
	return files
}

// GetStats returns directory and file counts for server reporting.
func (fs *FileSharer) GetStats() (directories, files int) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Count unique directories from Soulseek paths
	dirs := make(map[string]struct{})
	for path := range fs.files {
		dir := soulseekDir(path)
		if dir != "" {
			dirs[dir] = struct{}{}
		}
	}

	return len(dirs), len(fs.files)
}

// GetFolders returns the configured shared folders.
func (fs *FileSharer) GetFolders() []SharedFolder {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	folders := make([]SharedFolder, len(fs.folders))
	copy(folders, fs.folders)
	return folders
}

// SaveIndex writes the current file index to the given writer as JSON.
func (fs *FileSharer) SaveIndex(w io.Writer) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files := make([]*SharedFile, 0, len(fs.files))
	for _, f := range fs.files {
		files = append(files, f)
	}

	idx := persistedIndex{
		Version: currentIndexVersion,
		Folders: fs.folders,
		Files:   files,
		SavedAt: time.Now(),
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(idx)
}

// LoadIndex reads a previously saved index from the reader.
// Returns error if format is invalid or folder configuration has changed.
func (fs *FileSharer) LoadIndex(r io.Reader) error {
	var idx persistedIndex
	if err := json.NewDecoder(r).Decode(&idx); err != nil {
		return fmt.Errorf("decode index: %w", err)
	}

	if idx.Version != currentIndexVersion {
		return fmt.Errorf("unsupported index version: %d", idx.Version)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check if folder configuration matches
	if !foldersMatch(fs.folders, idx.Folders) {
		return errors.New("folder configuration has changed")
	}

	// Load files into index
	fs.files = make(map[string]*SharedFile, len(idx.Files))
	for _, f := range idx.Files {
		fs.files[f.SoulseekPath] = f
	}

	return nil
}

// localToSoulseek converts a local filesystem path to Soulseek format.
func localToSoulseek(localPath, folderPath, virtualPath string) string {
	// Get relative path from folder
	rel, err := filepath.Rel(folderPath, localPath)
	if err != nil {
		return ""
	}

	// Convert to forward slashes first (normalize)
	rel = filepath.ToSlash(rel)

	// Build Soulseek path with backslashes
	soulseekPath := virtualPath + `\` + strings.ReplaceAll(rel, "/", `\`)

	return soulseekPath
}

// soulseekDir returns the directory portion of a Soulseek path.
func soulseekDir(soulseekPath string) string {
	idx := strings.LastIndex(soulseekPath, `\`)
	if idx == -1 {
		return ""
	}
	return soulseekPath[:idx]
}

// foldersMatch checks if two folder slices have the same configuration.
func foldersMatch(a, b []SharedFolder) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]string, len(a))
	for _, f := range a {
		aMap[f.VirtualPath] = f.LocalPath
	}

	for _, f := range b {
		if aMap[f.VirtualPath] != f.LocalPath {
			return false
		}
	}

	return true
}
