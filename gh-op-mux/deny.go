package main

import "strings"

func deniedReason(argv []string) string {
	showToken := false
	for _, a := range argv {
		if a == "--show-token" || strings.HasPrefix(a, "--show-token=") {
			showToken = true
			break
		}
	}
	cmd := commandPath(argv)
	if len(cmd) >= 2 && cmd[0] == "auth" {
		switch cmd[1] {
		case "login":
			return "gh-op-mux: refused gh auth login (would persist credentials)"
		case "refresh":
			return "gh-op-mux: refused gh auth refresh (would persist credentials)"
		case "token":
			return "gh-op-mux: refused gh auth token (would print the token)"
		case "status":
			if showToken {
				return "gh-op-mux: refused gh auth status --show-token (would print the token)"
			}
		}
	}
	return ""
}

func commandPath(argv []string) []string {
	i := 0
	for i < len(argv) {
		a := argv[i]
		if a == "--" {
			i++
			break
		}
		if a == "--help" || a == "--version" || a == "--show-token" || strings.HasPrefix(a, "--show-token=") {
			i++
			continue
		}
		if a == "-R" || a == "--repo" || a == "-h" || a == "--hostname" {
			i += 2
			continue
		}
		if strings.HasPrefix(a, "--repo=") || strings.HasPrefix(a, "--hostname=") {
			i++
			continue
		}
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") && i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i += 2
				continue
			}
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			i++
			continue
		}
		break
	}
	if i > len(argv) {
		return nil
	}
	return argv[i:]
}
