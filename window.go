package main

import "strings"

func (s status) versions() string {
	var items []string
	if s.Installed != "" {
		items = append(items, "Installed: "+s.Installed)
	}
	if s.Available != "" && s.Available != s.Installed {
		items = append(items, "Latest: "+s.Available)
	}
	return strings.Join(items, "   ·   ")
}
func (s status) profile() string {
	if s.Username != "" {
		return "Signed in as " + s.Username
	}
	if s.AccountMessage != "" {
		return s.AccountMessage
	}
	return "Sign in to play."
}
func (s status) signInTitle() string {
	if s.Username != "" {
		return "Sign out"
	}
	return "Sign in or create an account"
}
func (s status) installTitle() string {
	if s.Installed != "" {
		return "Download update"
	}
	return "Download game"
}
