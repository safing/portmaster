package database

import (
	"time"
)

// Database holds information about a registered database.
type Database struct {
	Name         string
	Description  string
	StorageType  string
	ShadowDelete bool // Whether deleted records should be kept until purged.
	Registered   time.Time
	LastUpdated  time.Time
	LastLoaded   time.Time
}

// Loaded updates the LastLoaded timestamp.
func (db *Database) Loaded() {
	db.LastLoaded = time.Now().Round(time.Second)
}

// Updated updates the LastUpdated timestamp.
func (db *Database) Updated() {
	db.LastUpdated = time.Now().Round(time.Second)
}
