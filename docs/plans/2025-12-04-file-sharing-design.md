# Task 4.1: File Sharing Infrastructure Design

## Overview

Implement file indexing and sharing infrastructure for gosoulseek, enabling users to share files with other Soulseek peers.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Managed file index | Simpler API, handles path translation internally |
| Path translation | Explicit virtual paths | Predictable, user controls what peers see |
| Attribute extraction | Optional callback | No forced dependencies, user chooses audio library |
| Rescan strategy | Manual only | YAGNI - most shares are static, easy to add timer later |
| Access control | None | Deferred to Task 4.2 (Upload Requests) |
| Persistence | JSON with io.Reader/Writer | Human-readable, flexible, debuggable |

## Data Structures

```go
// SharedFolder represents a configured shared directory.
type SharedFolder struct {
    LocalPath   string // Absolute path on disk (e.g., "/home/user/Music")
    VirtualPath string // Path shown to peers (e.g., "Music")
}

// SharedFile represents an indexed file available for sharing.
type SharedFile struct {
    LocalPath    string               // Absolute path on disk
    SoulseekPath string               // Path as seen by peers (backslash-separated)
    Size         int64                // File size in bytes
    Extension    string               // File extension without dot (e.g., "mp3")
    Attributes   []peer.FileAttribute // Audio metadata (optional)
}

// FileSharer manages shared folders and file indexing.
type FileSharer struct {
    mu           sync.RWMutex
    folders      []SharedFolder
    files        map[string]*SharedFile  // keyed by SoulseekPath
    attrResolver func(string) []peer.FileAttribute
}

// Persisted index format
type persistedIndex struct {
    Version int            `json:"version"`
    Folders []SharedFolder `json:"folders"`
    Files   []*SharedFile  `json:"files"`
    SavedAt time.Time      `json:"saved_at"`
}
```

## Public API

```go
// Constructor
func NewFileSharer() *FileSharer

// Folder management
func (fs *FileSharer) AddSharedFolder(localPath, virtualPath string) error
func (fs *FileSharer) RemoveSharedFolder(virtualPath string)

// Attribute extraction
func (fs *FileSharer) SetAttributeResolver(fn func(localPath string) []peer.FileAttribute)

// Indexing
func (fs *FileSharer) Rescan() error

// Lookups
func (fs *FileSharer) GetFile(soulseekPath string) *SharedFile
func (fs *FileSharer) GetSharedFiles() []*SharedFile
func (fs *FileSharer) GetStats() (directories, files int)

// Persistence
func (fs *FileSharer) SaveIndex(w io.Writer) error
func (fs *FileSharer) LoadIndex(r io.Reader) error
```

## Path Translation

Soulseek paths use Windows-style backslashes. Translation rules:

1. Strip folder's `LocalPath` prefix from file's local path
2. Prepend `VirtualPath`
3. Replace forward slashes with backslashes
4. Extension extracted lowercase without dot

Example:
```
Local:       /home/user/Music/Jazz/Miles Davis/song.mp3
Folder:      LocalPath="/home/user/Music", VirtualPath="Music"
Soulseek:    Music\Jazz\Miles Davis\song.mp3
Extension:   mp3
```

## Client Integration

```go
// client/options.go
type Options struct {
    // ... existing fields ...
    FileSharer *FileSharer // If nil, no files are shared
}

// client/client.go - sendPostLoginConfig()
// Reports shared counts to server using FileSharer.GetStats()
```

## Usage Example

```go
sharer := client.NewFileSharer()
sharer.AddSharedFolder("/home/user/Music", "Music")
sharer.AddSharedFolder("/home/user/Audiobooks", "Audiobooks")

// Optional: set attribute extractor
sharer.SetAttributeResolver(func(path string) []peer.FileAttribute {
    // Use your preferred audio metadata library
    return extractAudioAttributes(path)
})

// Scan files
sharer.Rescan()

// Lookup
file := sharer.GetFile(`Music\Artist\song.mp3`)
fmt.Println(file.LocalPath) // /home/user/Music/Artist/song.mp3

// Persistence
f, _ := os.Create("shares.json")
sharer.SaveIndex(f)
f.Close()
```

## Files to Create/Modify

**New:**
- `client/fileshare.go` - FileSharer implementation
- `client/fileshare_test.go` - Tests

**Modified:**
- `client/options.go` - Add FileSharer field to Options
- `client/client.go` - Report stats on login

## Testing Strategy

Using `t.TempDir()` for real filesystem tests:

- `TestAddSharedFolder` - valid/invalid paths, duplicates
- `TestRescan` - recursive indexing, path building, attribute callback
- `TestGetFile` - lookup by Soulseek path
- `TestPathTranslation` - slash conversion, virtual paths
- `TestPersistence` - save/load round-trip, corruption handling
- `TestGetStats` - directory and file counting

## Future Considerations (Out of Scope)

- Browse request handling (Task 4.2)
- Access control callbacks (Task 4.2)
- Automatic periodic rescan
- File system watching (fsnotify)
- Locked directories support
