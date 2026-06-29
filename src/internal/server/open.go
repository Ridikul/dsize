package server

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// openInFileManager opens a path in the OS file manager. When reveal is true,
// the file is shown selected inside its parent folder; otherwise the path is
// opened as a directory. The path is passed as a separate argument (no shell),
// so it cannot be interpreted as a command.
func openInFileManager(path string, reveal bool) error {
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			return exec.Command("open", "-R", path).Start()
		}
		return exec.Command("open", path).Start()
	case "windows":
		if reveal {
			return exec.Command("explorer", "/select,"+path).Start()
		}
		return exec.Command("explorer", path).Start()
	default: // Linux/BSD: no portable "reveal"; open the containing folder.
		target := path
		if reveal {
			target = filepath.Dir(path)
		}
		return exec.Command("xdg-open", target).Start()
	}
}
