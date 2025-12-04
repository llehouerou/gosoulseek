package client

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/llehouerou/gosoulseek/messages/peer"
)

func TestNewFileSharer(t *testing.T) {
	fs := NewFileSharer()
	if fs == nil {
		t.Fatal("NewFileSharer returned nil")
	}
	if fs.files == nil {
		t.Error("files map not initialized")
	}
}

func TestAddSharedFolder(t *testing.T) {
	t.Run("valid folder", func(t *testing.T) {
		fs := NewFileSharer()
		dir := t.TempDir()

		err := fs.AddSharedFolder(dir, "Music")
		if err != nil {
			t.Fatalf("AddSharedFolder failed: %v", err)
		}

		folders := fs.GetFolders()
		if len(folders) != 1 {
			t.Fatalf("expected 1 folder, got %d", len(folders))
		}
		if folders[0].LocalPath != dir {
			t.Errorf("LocalPath = %q, want %q", folders[0].LocalPath, dir)
		}
		if folders[0].VirtualPath != "Music" {
			t.Errorf("VirtualPath = %q, want %q", folders[0].VirtualPath, "Music")
		}
	})

	t.Run("relative path", func(t *testing.T) {
		fs := NewFileSharer()
		err := fs.AddSharedFolder("relative/path", "Music")
		if err == nil {
			t.Error("expected error for relative path")
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		fs := NewFileSharer()
		err := fs.AddSharedFolder("/nonexistent/path/12345", "Music")
		if err == nil {
			t.Error("expected error for non-existent path")
		}
	})

	t.Run("file not directory", func(t *testing.T) {
		fs := NewFileSharer()
		dir := t.TempDir()
		file := filepath.Join(dir, "test.txt")
		if err := os.WriteFile(file, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := fs.AddSharedFolder(file, "Music")
		if err == nil {
			t.Error("expected error for file path")
		}
	})

	t.Run("empty virtual path", func(t *testing.T) {
		fs := NewFileSharer()
		dir := t.TempDir()
		err := fs.AddSharedFolder(dir, "")
		if err == nil {
			t.Error("expected error for empty virtual path")
		}
	})

	t.Run("virtual path with slashes", func(t *testing.T) {
		fs := NewFileSharer()
		dir := t.TempDir()

		err := fs.AddSharedFolder(dir, "Music/Rock")
		if err == nil {
			t.Error("expected error for virtual path with forward slash")
		}

		err = fs.AddSharedFolder(dir, `Music\Rock`)
		if err == nil {
			t.Error("expected error for virtual path with backslash")
		}
	})

	t.Run("duplicate virtual path", func(t *testing.T) {
		fs := NewFileSharer()
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		if err := fs.AddSharedFolder(dir1, "Music"); err != nil {
			t.Fatal(err)
		}

		err := fs.AddSharedFolder(dir2, "Music")
		if err == nil {
			t.Error("expected error for duplicate virtual path")
		}
	})
}

func TestRemoveSharedFolder(t *testing.T) {
	fs := NewFileSharer()
	dir := t.TempDir()

	// Create a file
	subdir := filepath.Join(dir, "Artist")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(subdir, "song.mp3")
	if err := os.WriteFile(file, []byte("fake audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Add and scan
	if err := fs.AddSharedFolder(dir, "Music"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rescan(); err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if fs.GetFile(`Music\Artist\song.mp3`) == nil {
		t.Fatal("file not found after scan")
	}

	// Remove folder
	fs.RemoveSharedFolder("Music")

	// Verify folder and files removed
	folders := fs.GetFolders()
	if len(folders) != 0 {
		t.Errorf("expected 0 folders, got %d", len(folders))
	}

	if fs.GetFile(`Music\Artist\song.mp3`) != nil {
		t.Error("file still exists after removing folder")
	}
}

func TestRescan(t *testing.T) {
	fs := NewFileSharer()
	dir := t.TempDir()

	// Create directory structure
	// Music/
	//   Artist1/
	//     song1.mp3
	//     song2.flac
	//   Artist2/
	//     album.mp3
	//   .hidden/
	//     secret.mp3
	//   .hiddenfile.mp3

	artist1 := filepath.Join(dir, "Artist1")
	artist2 := filepath.Join(dir, "Artist2")
	hidden := filepath.Join(dir, ".hidden")

	for _, d := range []string{artist1, artist2, hidden} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]int64{
		filepath.Join(artist1, "song1.mp3"):   1024,
		filepath.Join(artist1, "song2.flac"):  2048,
		filepath.Join(artist2, "album.mp3"):   4096,
		filepath.Join(hidden, "secret.mp3"):   512,
		filepath.Join(dir, ".hiddenfile.mp3"): 256,
	}

	for path, size := range files {
		data := make([]byte, size)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Add folder and scan
	if err := fs.AddSharedFolder(dir, "Music"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rescan(); err != nil {
		t.Fatal(err)
	}

	// Check indexed files
	tests := []struct {
		soulseekPath string
		wantSize     int64
		wantExt      string
	}{
		{`Music\Artist1\song1.mp3`, 1024, "mp3"},
		{`Music\Artist1\song2.flac`, 2048, "flac"},
		{`Music\Artist2\album.mp3`, 4096, "mp3"},
	}

	for _, tc := range tests {
		t.Run(tc.soulseekPath, func(t *testing.T) {
			f := fs.GetFile(tc.soulseekPath)
			if f == nil {
				t.Fatal("file not found")
			}
			if f.Size != tc.wantSize {
				t.Errorf("Size = %d, want %d", f.Size, tc.wantSize)
			}
			if f.Extension != tc.wantExt {
				t.Errorf("Extension = %q, want %q", f.Extension, tc.wantExt)
			}
		})
	}

	// Verify hidden files are skipped
	if fs.GetFile(`Music\.hidden\secret.mp3`) != nil {
		t.Error("hidden directory file should not be indexed")
	}
	if fs.GetFile(`Music\.hiddenfile.mp3`) != nil {
		t.Error("hidden file should not be indexed")
	}

	// Verify total count (3 files, not 5)
	allFiles := fs.GetSharedFiles()
	if len(allFiles) != 3 {
		t.Errorf("expected 3 files, got %d", len(allFiles))
	}
}

func TestRescanWithAttributeResolver(t *testing.T) {
	fs := NewFileSharer()
	dir := t.TempDir()

	file := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(file, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Set attribute resolver
	resolverCalled := false
	fs.SetAttributeResolver(func(path string) []peer.FileAttribute {
		resolverCalled = true
		if path != file {
			t.Errorf("resolver called with wrong path: %s", path)
		}
		return []peer.FileAttribute{
			{Type: peer.AttributeBitRate, Value: 320},
			{Type: peer.AttributeLength, Value: 180},
		}
	})

	if err := fs.AddSharedFolder(dir, "Music"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rescan(); err != nil {
		t.Fatal(err)
	}

	if !resolverCalled {
		t.Error("attribute resolver was not called")
	}

	f := fs.GetFile(`Music\song.mp3`)
	if f == nil {
		t.Fatal("file not found")
	}
	if len(f.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(f.Attributes))
	}
	if f.Attributes[0].Type != peer.AttributeBitRate || f.Attributes[0].Value != 320 {
		t.Errorf("unexpected bitrate attribute: %+v", f.Attributes[0])
	}
}

func TestGetFile(t *testing.T) {
	fs := NewFileSharer()
	dir := t.TempDir()

	file := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(file, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := fs.AddSharedFolder(dir, "Music"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rescan(); err != nil {
		t.Fatal(err)
	}

	t.Run("existing file", func(t *testing.T) {
		f := fs.GetFile(`Music\song.mp3`)
		if f == nil {
			t.Fatal("file not found")
		}
		if f.LocalPath != file {
			t.Errorf("LocalPath = %q, want %q", f.LocalPath, file)
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		f := fs.GetFile(`Music\nonexistent.mp3`)
		if f != nil {
			t.Error("expected nil for non-existent file")
		}
	})
}

func TestGetStats(t *testing.T) {
	fs := NewFileSharer()
	dir := t.TempDir()

	// Create structure:
	// Music/
	//   Artist1/
	//     song1.mp3
	//     song2.mp3
	//   Artist2/
	//     song3.mp3

	artist1 := filepath.Join(dir, "Artist1")
	artist2 := filepath.Join(dir, "Artist2")
	if err := os.MkdirAll(artist1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(artist2, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(artist1, "song1.mp3"),
		filepath.Join(artist1, "song2.mp3"),
		filepath.Join(artist2, "song3.mp3"),
	} {
		if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := fs.AddSharedFolder(dir, "Music"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rescan(); err != nil {
		t.Fatal(err)
	}

	dirs, files := fs.GetStats()
	if files != 3 {
		t.Errorf("files = %d, want 3", files)
	}
	// Directories: Music\Artist1, Music\Artist2
	if dirs != 2 {
		t.Errorf("dirs = %d, want 2", dirs)
	}
}

func TestPathTranslation(t *testing.T) {
	tests := []struct {
		localPath   string
		folderPath  string
		virtualPath string
		want        string
	}{
		{
			localPath:   "/home/user/Music/Artist/song.mp3",
			folderPath:  "/home/user/Music",
			virtualPath: "Music",
			want:        `Music\Artist\song.mp3`,
		},
		{
			localPath:   "/home/user/Music/song.mp3",
			folderPath:  "/home/user/Music",
			virtualPath: "MyMusic",
			want:        `MyMusic\song.mp3`,
		},
		{
			localPath:   "/home/user/Music/A/B/C/song.mp3",
			folderPath:  "/home/user/Music",
			virtualPath: "Tunes",
			want:        `Tunes\A\B\C\song.mp3`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := localToSoulseek(tc.localPath, tc.folderPath, tc.virtualPath)
			if got != tc.want {
				t.Errorf("localToSoulseek() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSoulseekDir(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{`Music\Artist\song.mp3`, `Music\Artist`},
		{`Music\song.mp3`, `Music`},
		{`song.mp3`, ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := soulseekDir(tc.path)
			if got != tc.want {
				t.Errorf("soulseekDir(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestPersistence(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		fs := NewFileSharer()
		dir := t.TempDir()

		// Create files
		subdir := filepath.Join(dir, "Artist")
		if err := os.Mkdir(subdir, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(subdir, "song.mp3")
		if err := os.WriteFile(file, []byte("audio data here"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add folder and scan
		if err := fs.AddSharedFolder(dir, "Music"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Rescan(); err != nil {
			t.Fatal(err)
		}

		// Save
		var buf bytes.Buffer
		if err := fs.SaveIndex(&buf); err != nil {
			t.Fatalf("SaveIndex: %v", err)
		}

		// Create new sharer with same folder config
		fs2 := NewFileSharer()
		if err := fs2.AddSharedFolder(dir, "Music"); err != nil {
			t.Fatal(err)
		}

		// Load
		if err := fs2.LoadIndex(&buf); err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}

		// Verify
		f := fs2.GetFile(`Music\Artist\song.mp3`)
		if f == nil {
			t.Fatal("file not found after load")
		}
		if f.Size != 15 {
			t.Errorf("Size = %d, want 15", f.Size)
		}
	})

	t.Run("folder config changed", func(t *testing.T) {
		fs := NewFileSharer()
		dir := t.TempDir()

		if err := fs.AddSharedFolder(dir, "Music"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Rescan(); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if err := fs.SaveIndex(&buf); err != nil {
			t.Fatal(err)
		}

		// Create new sharer with different folder config
		fs2 := NewFileSharer()
		dir2 := t.TempDir()
		if err := fs2.AddSharedFolder(dir2, "Music"); err != nil {
			t.Fatal(err)
		}

		// Load should fail
		err := fs2.LoadIndex(&buf)
		if err == nil {
			t.Error("expected error for changed folder config")
		}
	})

	t.Run("corrupt data", func(t *testing.T) {
		fs := NewFileSharer()
		buf := bytes.NewBufferString("not valid json")
		err := fs.LoadIndex(buf)
		if err == nil {
			t.Error("expected error for corrupt data")
		}
	})
}

func TestFoldersMatch(t *testing.T) {
	tests := []struct {
		name string
		a    []SharedFolder
		b    []SharedFolder
		want bool
	}{
		{
			name: "empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "same",
			a:    []SharedFolder{{LocalPath: "/a", VirtualPath: "A"}},
			b:    []SharedFolder{{LocalPath: "/a", VirtualPath: "A"}},
			want: true,
		},
		{
			name: "different length",
			a:    []SharedFolder{{LocalPath: "/a", VirtualPath: "A"}},
			b:    nil,
			want: false,
		},
		{
			name: "different path",
			a:    []SharedFolder{{LocalPath: "/a", VirtualPath: "A"}},
			b:    []SharedFolder{{LocalPath: "/b", VirtualPath: "A"}},
			want: false,
		},
		{
			name: "different order",
			a: []SharedFolder{
				{LocalPath: "/a", VirtualPath: "A"},
				{LocalPath: "/b", VirtualPath: "B"},
			},
			b: []SharedFolder{
				{LocalPath: "/b", VirtualPath: "B"},
				{LocalPath: "/a", VirtualPath: "A"},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := foldersMatch(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("foldersMatch() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMultipleFolders(t *testing.T) {
	fs := NewFileSharer()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Create files in both directories
	file1 := filepath.Join(dir1, "song1.mp3")
	file2 := filepath.Join(dir2, "song2.mp3")
	if err := os.WriteFile(file1, []byte("audio1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("audio2"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Add both folders
	if err := fs.AddSharedFolder(dir1, "Music"); err != nil {
		t.Fatal(err)
	}
	if err := fs.AddSharedFolder(dir2, "Audiobooks"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rescan(); err != nil {
		t.Fatal(err)
	}

	// Verify both files indexed
	if fs.GetFile(`Music\song1.mp3`) == nil {
		t.Error("song1.mp3 not found")
	}
	if fs.GetFile(`Audiobooks\song2.mp3`) == nil {
		t.Error("song2.mp3 not found")
	}

	// Verify stats
	dirs, files := fs.GetStats()
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}
	if dirs != 2 {
		t.Errorf("dirs = %d, want 2", dirs)
	}
}
