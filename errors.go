package main

import "errors"

var (
	ErrUserExists         = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotFound           = errors.New("not found")
)
