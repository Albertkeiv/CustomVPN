package main

import "time"

// User represents a user
type User struct {
	ID       string
	Login    string
	Password string
}

// AuthToken represents an authentication token
type AuthToken struct {
	Value     string
	UserLogin string
	IssuedAt  time.Time
	ExpiresAt *time.Time // null for long-lived
}