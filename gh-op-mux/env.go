package main

import (
	"os"
	"strings"
)

var exactAllow = map[string]bool{
	"PATH":            true,
	"HOME":            true,
	"USER":            true,
	"SHELL":           true,
	"TERM":            true,
	"EDITOR":          true,
	"VISUAL":          true,
	"PAGER":           true,
	"GH_PAGER":        true,
	"NO_COLOR":        true,
	"CLICOLOR":        true,
	"CLICOLOR_FORCE":  true,
	"XDG_CACHE_HOME":  true,
	"XDG_CONFIG_HOME": true,
	"XDG_DATA_HOME":   true,
	"XDG_STATE_HOME":  true,
}

var exactStrip = map[string]bool{
	"GH_TOKEN":                true,
	"GITHUB_TOKEN":            true,
	"GH_ENTERPRISE_TOKEN":     true,
	"GITHUB_ENTERPRISE_TOKEN": true,
}

func strippedEnv(name string) bool {
	if exactStrip[name] {
		return true
	}
	return strings.HasPrefix(name, "OP_")
}

func allowedEnv(name string) bool {
	if strippedEnv(name) {
		return false
	}
	if exactAllow[name] {
		return true
	}
	if strings.HasPrefix(name, "GH_") || strings.HasPrefix(name, "GIT_") {
		return true
	}
	return false
}

func collectClientEnv() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if allowedEnv(k) {
			out[k] = v
		}
	}
	return out
}

func childEnv(fromClient map[string]string) []string {
	var env []string
	for k, v := range fromClient {
		if strippedEnv(k) {
			continue
		}
		if !allowedEnv(k) {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
}
