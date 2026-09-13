// Package models demonstrates inherited audit/metadata and nested descriptors.
package models

import "github.com/ctolon/esodm"

//go:generate go run ../../cmd/esodmgen -type Product

// Review is stored as a nested document.
type Review struct {
	Text   string `json:"text" es:"type=text"`
	Rating int    `json:"rating"`
}

// Product embeds opt-in timestamps and ID/routing metadata.
type Product struct {
	esodm.Timestamps
	esodm.DocumentMeta
	Name     string         `json:"name" es:"type=text"`
	Price    int            `json:"price"`
	Reviews  []Review       `json:"reviews" es:"type=nested"`
	Location esodm.GeoPoint `json:"location"`
}

// IndexName declares the default index used by NewSchemaFor.
func (*Product) IndexName() string { return "products" }
