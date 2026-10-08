package xkeen

import "strings"

// Entware packages must win over the firmware's BusyBox applets. Keep extra
// operator tool directories after them, without introducing cwd lookup.
func withEntwarePath(env []string) []string {
	result := make([]string, 0, len(env)+1)
	path := "/usr/sbin:/usr/bin:/sbin:/bin"
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
		} else {
			result = append(result, entry)
		}
	}
	if path == "" {
		path = "/usr/sbin:/usr/bin:/sbin:/bin"
	}
	parts := []string{"/opt/bin", "/opt/sbin"}
	seen := map[string]bool{"/opt/bin": true, "/opt/sbin": true}
	for _, directory := range strings.Split(path, ":") {
		if directory != "" && !seen[directory] {
			parts = append(parts, directory)
			seen[directory] = true
		}
	}
	return append(result, "PATH="+strings.Join(parts, ":"))
}
