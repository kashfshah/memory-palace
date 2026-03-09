package main

import (
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Zotero RDF export format structures
type rdfDocument struct {
	XMLName    xml.Name     `xml:"RDF"`
	XMLNS      string       `xml:"xmlns,attr"`
	XMLNSRDF   string       `xml:"xmlns:rdf,attr"`
	XMLNSDC    string       `xml:"xmlns:dc,attr"`
	XMLNSDCTERMS string     `xml:"xmlns:dcterms,attr"`
	XMLNSBIB   string       `xml:"xmlns:bib,attr"`
	XMLNSZ     string       `xml:"xmlns:z,attr"`
	XMLNSFOAF  string       `xml:"xmlns:foaf,attr"`
	Items      []rdfItem    `xml:"bib:Document"`
}

type rdfItem struct {
	About      string       `xml:"rdf:about,attr"`
	ItemType   rdfType      `xml:"z:itemType"`
	Title      rdfValue     `xml:"dc:title"`
	Identifier rdfValue     `xml:"dc:identifier"`
	Subject    []rdfValue   `xml:"dc:subject"`
	Date       rdfValue     `xml:"dcterms:dateSubmitted,omitempty"`
}

type rdfType struct {
	Value string `xml:",chardata"`
}

type rdfValue struct {
	Value string `xml:",chardata"`
}

// generateZoteroRDF creates a Zotero-importable RDF file from Safari bookmarks.
func generateZoteroRDF(memoryDBPath string, batchSize int) error {
	home, _ := os.UserHomeDir()
	zoteroDBPath := home + "/Zotero/zotero.sqlite"

	log.Println("Loading existing Zotero URLs for deduplication...")
	existing, err := loadExistingZoteroURLs(zoteroDBPath)
	if err != nil {
		return fmt.Errorf("load zotero urls: %w", err)
	}
	log.Printf("  Found %d existing URLs in Zotero", len(existing))

	log.Println("Loading Safari bookmarks from Memory Palace...")
	bookmarks, err := loadSafariBookmarks(memoryDBPath)
	if err != nil {
		return fmt.Errorf("load bookmarks: %w", err)
	}
	log.Printf("  Found %d Safari bookmarks", len(bookmarks))

	var toImport []bookmark
	for _, b := range bookmarks {
		if !existing[normalizeURL(b.URL)] {
			toImport = append(toImport, b)
		}
	}
	log.Printf("  %d new bookmarks after deduplication", len(toImport))

	if batchSize > 0 && len(toImport) > batchSize {
		toImport = toImport[:batchSize]
	}

	// Build RDF document
	doc := rdfDocument{
		XMLNS:      "http://purl.org/net/biblio#",
		XMLNSRDF:   "http://www.w3.org/1999/02/22-rdf-syntax-ns#",
		XMLNSDC:    "http://purl.org/dc/elements/1.1/",
		XMLNSDCTERMS: "http://purl.org/dc/terms/",
		XMLNSBIB:   "http://purl.org/net/biblio#",
		XMLNSZ:     "http://www.zotero.org/namespaces/export#",
		XMLNSFOAF:  "http://xmlns.com/foaf/0.1/",
	}

	collectionCounts := map[string]int{}
	for _, b := range toImport {
		col := classifyURL(b.URL)
		tags := []rdfValue{{Value: "safari-import"}}

		// Add collection name as a tag (Zotero RDF import doesn't support collections directly)
		colName := collectionNameFromID(col)
		if colName != "" {
			tags = append(tags, rdfValue{Value: colName})
			collectionCounts[colName]++
		} else {
			collectionCounts["(uncategorized)"]++
		}

		item := rdfItem{
			About:      b.URL,
			ItemType:   rdfType{Value: "webpage"},
			Title:      rdfValue{Value: b.Title},
			Identifier: rdfValue{Value: b.URL},
			Subject:    tags,
			Date:       rdfValue{Value: time.Now().Format("2006-01-02")},
		}
		doc.Items = append(doc.Items, item)
	}

	// Write RDF file
	outPath := "data/safari-bookmarks-for-zotero.rdf"
	if err := os.MkdirAll("data", 0755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}

	log.Printf("\nRDF export written to %s (%d items)", outPath, len(toImport))
	log.Println("Import in Zotero: File → Import → select the .rdf file")
	log.Println("\nCollection distribution (via tags — assign collections after import):")
	for col, count := range collectionCounts {
		log.Printf("  %-25s %d", col, count)
	}
	log.Println("\nAfter import, use Zotero search for tag 'Computing' etc. to bulk-move to collections.")
	return nil
}

// collectionNameFromID maps collection IDs to human-readable names.
func collectionNameFromID(id string) string {
	names := map[string]string{
		"C1": "3D Printing", "C2": "Dev", "C3": "Aqua", "C4": "Gaming",
		"C5": "Audio-Visual", "C7": "Electronics", "C9": "Ops",
		"C10": "News", "C11": "Psychology", "C12": "Mathematics",
		"C13": "Make", "C14": "Religion", "C15": "Science",
		"C16": "Computing", "C17": "SDF", "C18": "Law",
		"C20": "Perma", "C21": "Tools", "C23": "Politics",
		"C24": "plan9", "C25": "Human Rights", "C26": "psh-skos",
	}
	return names[id]
}

// Also generate a simpler CSV for reference
func generateMigrationCSV(memoryDBPath string, batchSize int) error {
	home, _ := os.UserHomeDir()
	zoteroDBPath := home + "/Zotero/zotero.sqlite"

	existing, err := loadExistingZoteroURLs(zoteroDBPath)
	if err != nil {
		return err
	}
	bookmarks, err := loadSafariBookmarks(memoryDBPath)
	if err != nil {
		return err
	}

	var toImport []bookmark
	for _, b := range bookmarks {
		if !existing[normalizeURL(b.URL)] {
			toImport = append(toImport, b)
		}
	}
	if batchSize > 0 && len(toImport) > batchSize {
		toImport = toImport[:batchSize]
	}

	outPath := "data/safari-bookmarks-for-zotero.csv"
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString("Title,Url,Tags\n")
	for _, b := range toImport {
		col := classifyURL(b.URL)
		tags := "safari-import"
		if name := collectionNameFromID(col); name != "" {
			tags += ";" + name
		}
		// CSV escape: double-quote fields containing commas or quotes
		title := strings.ReplaceAll(b.Title, "\"", "\"\"")
		f.WriteString(fmt.Sprintf("\"%s\",\"%s\",\"%s\"\n", title, b.URL, tags))
	}

	log.Printf("CSV export written to %s (%d items)", outPath, len(toImport))
	return nil
}
