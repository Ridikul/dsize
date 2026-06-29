package server

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// errPickerUnsupported means no native folder-dialog tool was found, so the UI
// should fall back to a path text field.
var errPickerUnsupported = errors.New("no native folder dialog available")

// pickFolder opens the OS-native folder chooser and returns the selected
// absolute path. An empty string with a nil error means the user cancelled.
// It blocks until the dialog is dismissed.
func pickFolder() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		// osascript prints the POSIX path on success; it exits non-zero when
		// the user cancels, which we treat as "no selection".
		out, err := exec.Command("osascript", "-e",
			`POSIX path of (choose folder with prompt "Select a folder to scan")`).Output()
		if err != nil {
			return "", nil
		}
		return strings.TrimRight(string(out), "\r\n"), nil

	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`$d = New-Object System.Windows.Forms.FolderBrowserDialog; ` +
			`if ($d.ShowDialog() -eq 'OK') { [Console]::Out.Write($d.SelectedPath) }`
		out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
		if err != nil {
			return "", nil
		}
		return strings.TrimRight(string(out), "\r\n"), nil

	default: // linux/bsd
		if _, err := exec.LookPath("zenity"); err != nil {
			return "", errPickerUnsupported
		}
		out, err := exec.Command("zenity", "--file-selection", "--directory",
			"--title=Select a folder to scan").Output()
		if err != nil {
			return "", nil // cancelled
		}
		return strings.TrimRight(string(out), "\r\n"), nil
	}
}
