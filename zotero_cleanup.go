package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// generateCleanupScripts produces JavaScript snippets for Zotero's JS console.
// Run them in Zotero: Tools → Developer → Run JavaScript.
func generateCleanupScripts(outDir string) error {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Zotero/zotero.sqlite"

	conn, err := sql.Open("sqlite", dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	steps := []struct {
		name string
		fn   func(*sql.DB, string) error
	}{
		{"delete test items", genDeleteTestItems},
		{"deduplicate URLs", genDeduplicateURLs},
		{"merge duplicate tags", genMergeTags},
		{"fix missing titles", genFixTitles},
		{"classify uncategorized items", genClassifyUncategorized},
		{"remove orphan attachments", genRemoveOrphanAttachments},
		{"normalize URLs (strip tracking params)", genNormalizeURLs},
		{"consolidate semantic tags", genConsolidateSemanticTags},
		{"detect dead links", genDetectDeadLinks},
	}

	for _, step := range steps {
		log.Printf("Generating: %s...", step.name)
		if err := step.fn(conn, outDir); err != nil {
			log.Printf("  WARN: %v", err)
		}
	}

	log.Printf("\nCleanup scripts written to %s/", outDir)
	log.Println("Run each script in Zotero: Tools → Developer → Run JavaScript")
	log.Println("Recommended order: 01 → 02 → 03 → 04 → 05 → 06 → 07 → 08 → 09")
	return nil
}

func genDeleteTestItems(conn *sql.DB, outDir string) error {
	rows, err := conn.Query(`
		SELECT DISTINCT i.itemID FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN itemData id ON i.itemID = id.itemID
		LEFT JOIN fields f ON id.fieldID = f.fieldID AND f.fieldName = 'url'
		LEFT JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE it.typeName NOT IN ('attachment','note')
			AND i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND (idv.value LIKE '%example.com/test%' OR i.itemID >= 2644)
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, fmt.Sprintf("%d", id))
	}

	if len(ids) == 0 {
		log.Println("  No test items found")
		return nil
	}

	script := fmt.Sprintf(`// Delete %d test items
var ids = [%s];
var deleted = 0;
for (let id of ids) {
    let item = await Zotero.Items.getAsync(id);
    if (item) {
        await item.eraseTx();
        deleted++;
    }
}
return deleted + " test items deleted";
`, len(ids), strings.Join(ids, ", "))

	return os.WriteFile(outDir+"/01-delete-test-items.js", []byte(script), 0644)
}

func genDeduplicateURLs(conn *sql.DB, outDir string) error {
	rows, err := conn.Query(`
		SELECT idv.value as url, GROUP_CONCAT(i.itemID) as item_ids, COUNT(*) as cnt
		FROM items i
		JOIN itemData id ON i.itemID = id.itemID
		JOIN fields f ON id.fieldID = f.fieldID AND f.fieldName = 'url'
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE i.itemID NOT IN (SELECT itemID FROM deletedItems)
		GROUP BY idv.value HAVING cnt > 1
		ORDER BY cnt DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var dupGroups []string
	totalDups := 0
	for rows.Next() {
		var url, itemIDs string
		var cnt int
		rows.Scan(&url, &itemIDs, &cnt)
		dupGroups = append(dupGroups, fmt.Sprintf(`  {url: %q, ids: [%s]}`, url, itemIDs))
		totalDups += cnt - 1
	}

	if len(dupGroups) == 0 {
		log.Println("  No duplicates found")
		return nil
	}

	log.Printf("  Found %d duplicate groups (%d items to remove)", len(dupGroups), totalDups)

	script := fmt.Sprintf(`// Deduplicate %d URL groups (keep oldest, merge tags/collections, delete rest)
var dupGroups = [
%s
];

var deleted = 0;
for (let group of dupGroups) {
    let items = [];
    for (let id of group.ids) {
        let item = await Zotero.Items.getAsync(id);
        if (item) items.push(item);
    }
    if (items.length < 2) continue;

    // Keep the oldest item (first added)
    items.sort((a, b) => a.dateAdded.localeCompare(b.dateAdded));
    let keeper = items[0];

    // Merge tags and collections from duplicates into keeper
    for (let i = 1; i < items.length; i++) {
        let dup = items[i];
        for (let tag of dup.getTags()) {
            keeper.addTag(tag.tag, tag.type);
        }
        for (let colID of dup.getCollections()) {
            keeper.addToCollection(colID);
        }
    }
    await keeper.saveTx();

    // Delete duplicates
    for (let i = 1; i < items.length; i++) {
        await items[i].eraseTx();
        deleted++;
    }
}
return deleted + " duplicate items removed from " + dupGroups.length + " URL groups";
`, len(dupGroups), strings.Join(dupGroups, ",\n"))

	return os.WriteFile(outDir+"/02-deduplicate-urls.js", []byte(script), 0644)
}

func genMergeTags(conn *sql.DB, outDir string) error {
	rows, err := conn.Query(`
		SELECT ltag, GROUP_CONCAT(name, '|') as variants, GROUP_CONCAT(tagID, '|') as tagIDs
		FROM (SELECT DISTINCT LOWER(t.name) as ltag, t.name, t.tagID
			FROM tags t
			JOIN itemTags itg ON t.tagID = itg.tagID)
		GROUP BY ltag HAVING COUNT(*) > 1
		ORDER BY ltag
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type tagGroup struct {
		canonical string
		variants  string
	}
	var groups []tagGroup
	for rows.Next() {
		var ltag, variants, tagIDs string
		rows.Scan(&ltag, &variants, &tagIDs)
		parts := strings.Split(variants, "|")
		canonical := parts[0]
		for _, p := range parts {
			if len(p) > 0 && p[0] >= 'A' && p[0] <= 'Z' {
				canonical = p
				break
			}
		}
		groups = append(groups, tagGroup{canonical: canonical, variants: variants})
	}

	if len(groups) == 0 {
		log.Println("  No duplicate tags found")
		return nil
	}

	log.Printf("  Found %d tag groups to merge", len(groups))

	var entries []string
	for _, g := range groups {
		parts := strings.Split(g.variants, "|")
		var quoted []string
		for _, p := range parts {
			if p != g.canonical {
				quoted = append(quoted, fmt.Sprintf("%q", p))
			}
		}
		entries = append(entries, fmt.Sprintf(`  {canonical: %q, variants: [%s]}`,
			g.canonical, strings.Join(quoted, ", ")))
	}

	script := fmt.Sprintf(`// Merge %d case-duplicate tag groups
var tagGroups = [
%s
];

var merged = 0;
for (let group of tagGroups) {
    for (let variant of group.variants) {
        let s = new Zotero.Search();
        s.addCondition('tag', 'is', variant);
        let ids = await s.search();
        for (let id of ids) {
            let item = await Zotero.Items.getAsync(id);
            if (!item) continue;
            item.removeTag(variant);
            item.addTag(group.canonical);
            await item.saveTx();
            merged++;
        }
    }
}
return merged + " tag assignments normalized across " + tagGroups.length + " groups";
`, len(groups), strings.Join(entries, ",\n"))

	return os.WriteFile(outDir+"/03-merge-tags.js", []byte(script), 0644)
}

func genFixTitles(conn *sql.DB, outDir string) error {
	rows, err := conn.Query(`
		SELECT i.itemID,
			GROUP_CONCAT(CASE WHEN f.fieldName='url' THEN idv.value END) as url
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN itemData id ON i.itemID = id.itemID
		LEFT JOIN fields f ON id.fieldID = f.fieldID
		LEFT JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE it.typeName NOT IN ('attachment','note')
			AND i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND i.itemID NOT IN (
				SELECT id2.itemID FROM itemData id2
				JOIN fields f2 ON id2.fieldID = f2.fieldID
				JOIN itemDataValues idv2 ON id2.valueID = idv2.valueID
				WHERE f2.fieldName='title' AND idv2.value <> ''
			)
		GROUP BY i.itemID
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id int64
		var url sql.NullString
		rows.Scan(&id, &url)
		ids = append(ids, fmt.Sprintf("%d", id))
	}

	if len(ids) == 0 {
		log.Println("  No titleless items found")
		return nil
	}

	log.Printf("  Found %d items without titles", len(ids))

	script := fmt.Sprintf(`// Fix %d items with missing titles (fetches from URL)
var ids = [%s];

var fixed = 0;
for (let id of ids) {
    let item = await Zotero.Items.getAsync(id);
    if (!item) continue;
    let url = item.getField('url');
    if (url) {
        try {
            let resp = await Zotero.HTTP.request('GET', url, {timeout: 10000});
            let match = resp.responseText.match(/<title[^>]*>(.*?)<\/title>/i);
            if (match && match[1]) {
                let title = match[1].replace(/&amp;/g, '&').replace(/&lt;/g, '<')
                    .replace(/&gt;/g, '>').replace(/&#39;/g, "'")
                    .replace(/&quot;/g, '"').trim();
                item.setField('title', title);
                await item.saveTx();
                Zotero.log("Fixed title for " + id + ": " + title);
                fixed++;
            }
        } catch (e) {
            Zotero.log("Could not fetch " + url + ": " + e.message);
        }
    }
}
return fixed + " titles fixed out of " + ids.length + " items";
`, len(ids), strings.Join(ids, ", "))

	return os.WriteFile(outDir+"/04-fix-titles.js", []byte(script), 0644)
}

// genClassifyUncategorized assigns uncategorized items to collections by URL domain.
func genClassifyUncategorized(conn *sql.DB, outDir string) error {
	// Get items with URLs but no collection assignment
	rows, err := conn.Query(`
		SELECT i.itemID, idv.value as url
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		JOIN itemData id ON i.itemID = id.itemID
		JOIN fields f ON id.fieldID = f.fieldID AND f.fieldName = 'url'
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE it.typeName NOT IN ('attachment','note')
			AND i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND i.itemID NOT IN (SELECT itemID FROM collectionItems)
			AND idv.value LIKE 'http%'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type classified struct {
		id           int64
		collectionID string
	}
	var items []classified
	unclassified := 0
	for rows.Next() {
		var id int64
		var url string
		rows.Scan(&id, &url)
		col := classifyURL(url)
		if col != "" {
			items = append(items, classified{id: id, collectionID: col})
		} else {
			unclassified++
		}
	}

	if len(items) == 0 {
		log.Printf("  No classifiable items found (%d remain uncategorized)", unclassified)
		return nil
	}

	log.Printf("  Found %d items to classify (%d remain uncategorized — no domain match)", len(items), unclassified)

	// Group by collection for the JS script
	byCollection := map[string][]string{}
	for _, item := range items {
		byCollection[item.collectionID] = append(byCollection[item.collectionID],
			fmt.Sprintf("%d", item.id))
	}

	var entries []string
	for col, ids := range byCollection {
		entries = append(entries, fmt.Sprintf(`  {collection: %q, ids: [%s]}`,
			col, strings.Join(ids, ", ")))
	}

	// Get collection name mapping for logging
	colNames, err := conn.Query(`SELECT collectionID, collectionName FROM collections`)
	nameMap := map[int]string{}
	if err == nil {
		defer colNames.Close()
		for colNames.Next() {
			var id int
			var name string
			colNames.Scan(&id, &name)
			nameMap[id] = name
		}
	}

	var summary []string
	for col, ids := range byCollection {
		// Parse numeric ID from "C5" format
		numStr := col[1:]
		name := col
		for id, n := range nameMap {
			if fmt.Sprintf("%d", id) == numStr {
				name = n
				break
			}
		}
		summary = append(summary, fmt.Sprintf("    %-25s %d items", name+" ("+col+")", len(ids)))
	}
	log.Printf("  Distribution:\n%s", strings.Join(summary, "\n"))

	script := fmt.Sprintf(`// Classify %d uncategorized items into collections by URL domain
var groups = [
%s
];

var classified = 0;
for (let group of groups) {
    // Resolve collection from key like "C5" → numeric collectionID
    let colKey = group.collection;
    let colID;
    if (colKey.startsWith('C')) {
        colID = parseInt(colKey.substring(1));
    }
    if (!colID) continue;

    for (let id of group.ids) {
        let item = await Zotero.Items.getAsync(id);
        if (!item) continue;
        item.addToCollection(colID);
        await item.saveTx();
        classified++;
    }
}
return classified + " items classified into collections";
`, len(items), strings.Join(entries, ",\n"))

	return os.WriteFile(outDir+"/05-classify-uncategorized.js", []byte(script), 0644)
}

// genRemoveOrphanAttachments finds attachments whose parent items no longer exist.
func genRemoveOrphanAttachments(conn *sql.DB, outDir string) error {
	var orphanCount int
	err := conn.QueryRow(`
		SELECT COUNT(*) FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		WHERE it.typeName = 'attachment'
			AND i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND i.itemID NOT IN (
				SELECT ia.itemID FROM itemAttachments ia
				WHERE ia.parentItemID IN (
					SELECT i2.itemID FROM items i2
					WHERE i2.itemID NOT IN (SELECT itemID FROM deletedItems)
				)
				OR ia.parentItemID IS NULL
			)
	`).Scan(&orphanCount)
	if err != nil {
		return err
	}

	// Also check for standalone attachments with broken parent references
	rows, err := conn.Query(`
		SELECT ia.itemID, ia.parentItemID FROM itemAttachments ia
		WHERE ia.parentItemID IS NOT NULL
			AND ia.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND ia.parentItemID IN (SELECT itemID FROM deletedItems)
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var orphanIDs []string
	for rows.Next() {
		var id, parentID int64
		rows.Scan(&id, &parentID)
		orphanIDs = append(orphanIDs, fmt.Sprintf("%d", id))
	}

	// Also find note orphans
	noteRows, err := conn.Query(`
		SELECT in2.itemID FROM itemNotes in2
		WHERE in2.parentItemID IS NOT NULL
			AND in2.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND in2.parentItemID IN (SELECT itemID FROM deletedItems)
	`)
	if err == nil {
		defer noteRows.Close()
		for noteRows.Next() {
			var id int64
			noteRows.Scan(&id)
			orphanIDs = append(orphanIDs, fmt.Sprintf("%d", id))
		}
	}

	if len(orphanIDs) == 0 {
		log.Println("  No orphan attachments found")
		// Still generate the script for standalone attachment cleanup
		script := `// Check for orphan attachments and notes
// Zotero's built-in integrity check covers most cases
// Run: Tools → Developer → Run Integrity Check

// This script finds attachments with missing files
var items = await Zotero.Items.getAll(1);
var missing = 0;
for (let item of items) {
    if (!item.isAttachment()) continue;
    if (item.attachmentLinkMode === Zotero.Attachments.LINK_MODE_LINKED_URL) continue;
    let path = await item.getFilePathAsync();
    if (path) {
        let exists = await OS.File.exists(path);
        if (!exists) {
            Zotero.log("Missing file for item " + item.id + ": " + path);
            missing++;
        }
    }
}
return missing + " attachments have missing files (review in Zotero UI)";
`
		return os.WriteFile(outDir+"/06-remove-orphan-attachments.js", []byte(script), 0644)
	}

	log.Printf("  Found %d orphan attachments/notes", len(orphanIDs))

	script := fmt.Sprintf(`// Remove %d orphan attachments/notes whose parents were deleted
var ids = [%s];
var removed = 0;
for (let id of ids) {
    let item = await Zotero.Items.getAsync(id);
    if (item) {
        await item.eraseTx();
        removed++;
    }
}

// Also check for attachments with missing files
var items = await Zotero.Items.getAll(1);
var missing = 0;
for (let item of items) {
    if (!item.isAttachment()) continue;
    if (item.attachmentLinkMode === Zotero.Attachments.LINK_MODE_LINKED_URL) continue;
    let path = await item.getFilePathAsync();
    if (path) {
        let exists = await OS.File.exists(path);
        if (!exists) {
            Zotero.log("Missing file for item " + item.id + ": " + path);
            missing++;
        }
    }
}
return removed + " orphans removed, " + missing + " attachments have missing files";
`, len(orphanIDs), strings.Join(orphanIDs, ", "))

	return os.WriteFile(outDir+"/06-remove-orphan-attachments.js", []byte(script), 0644)
}

// genNormalizeURLs strips tracking parameters from URLs.
func genNormalizeURLs(conn *sql.DB, outDir string) error {
	// Find URLs with tracking parameters
	rows, err := conn.Query(`
		SELECT i.itemID, idv.value as url
		FROM items i
		JOIN itemData id ON i.itemID = id.itemID
		JOIN fields f ON id.fieldID = f.fieldID AND f.fieldName = 'url'
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND (idv.value LIKE '%utm_%'
				OR idv.value LIKE '%fbclid%'
				OR idv.value LIKE '%gclid%'
				OR idv.value LIKE '%mc_cid%'
				OR idv.value LIKE '%mc_eid%'
				OR idv.value LIKE '%ref=%'
				OR idv.value LIKE '%source=%'
				OR idv.value LIKE '%&amp;%')
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type dirtyURL struct {
		id  int64
		url string
	}
	var dirty []dirtyURL
	for rows.Next() {
		var d dirtyURL
		rows.Scan(&d.id, &d.url)
		dirty = append(dirty, d)
	}

	if len(dirty) == 0 {
		log.Println("  No URLs with tracking parameters found")
		return nil
	}

	log.Printf("  Found %d URLs with tracking parameters", len(dirty))

	var entries []string
	for _, d := range dirty {
		entries = append(entries, fmt.Sprintf("  %d", d.id))
	}

	script := fmt.Sprintf(`// Normalize %d URLs by stripping tracking parameters
var trackingParams = new Set([
    'utm_source', 'utm_medium', 'utm_campaign', 'utm_term', 'utm_content',
    'utm_id', 'utm_cid', 'utm_reader', 'utm_name', 'utm_social', 'utm_social-type',
    'fbclid', 'gclid', 'gclsrc', 'dclid', 'gbraid', 'wbraid',
    'mc_cid', 'mc_eid',
    'msclkid', 'twclid', 'li_fat_id',
    '_ga', '_gl', '_hsenc', '_hsmi', '_openstat',
    'yclid', 'ymclid',
    'ref', 'ref_src', 'ref_url'
]);

var ids = [%s];

function cleanURL(url) {
    // Fix HTML entities first
    url = url.replace(/&amp;/g, '&');

    try {
        let u = new URL(url);
        let changed = false;
        for (let param of [...u.searchParams.keys()]) {
            if (trackingParams.has(param) || param.startsWith('utm_')) {
                u.searchParams.delete(param);
                changed = true;
            }
        }
        // Remove trailing ? if no params left
        if (changed) {
            let clean = u.toString();
            if (clean.endsWith('?')) clean = clean.slice(0, -1);
            return clean;
        }
    } catch (e) {
        // Not a valid URL, skip
    }
    // Still fix &amp; even if no tracking params
    if (url.includes('&amp;')) return url.replace(/&amp;/g, '&');
    return null;
}

var cleaned = 0;
for (let id of ids) {
    let item = await Zotero.Items.getAsync(id);
    if (!item) continue;
    let url = item.getField('url');
    let clean = cleanURL(url);
    if (clean && clean !== url) {
        item.setField('url', clean);
        await item.saveTx();
        cleaned++;
    }
}
return cleaned + " URLs normalized out of " + ids.length + " candidates";
`, len(dirty), strings.Join(entries, ",\n"))

	return os.WriteFile(outDir+"/07-normalize-urls.js", []byte(script), 0644)
}

// genConsolidateSemanticTags merges semantically equivalent tags.
func genConsolidateSemanticTags(conn *sql.DB, outDir string) error {
	// Build a map of all tags with their item counts
	rows, err := conn.Query(`
		SELECT t.name, COUNT(itg.itemID) as cnt
		FROM tags t
		JOIN itemTags itg ON t.tagID = itg.tagID
		GROUP BY t.name
		ORDER BY cnt DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	tagCounts := map[string]int{}
	for rows.Next() {
		var name string
		var cnt int
		rows.Scan(&name, &cnt)
		tagCounts[name] = cnt
	}

	// Define semantic merge groups: canonical → variants
	// These represent tags that mean the same thing
	semanticGroups := map[string][]string{
		"AI":                  {"Artificial Intelligence", "artificial intelligence", "A.I.", "ai", "machine learning", "ML"},
		"Programming":        {"programming", "coding", "software development", "software engineering"},
		"JavaScript":         {"javascript", "js", "JS", "node.js", "nodejs", "Node.js"},
		"Python":             {"python", "Python3", "python3"},
		"Linux":              {"linux", "GNU/Linux", "gnu/linux"},
		"Privacy":            {"privacy", "data privacy", "digital privacy"},
		"Security":           {"security", "cybersecurity", "infosec", "information security"},
		"Open Source":        {"open source", "open-source", "FOSS", "foss", "free software", "OSS", "oss"},
		"3D Printing":        {"3d printing", "3D printing", "3d-printing", "additive manufacturing"},
		"Self-hosted":        {"self-hosted", "selfhosted", "self hosted"},
		"Hardware":           {"hardware", "Hardware"},
		"Environment":        {"environment", "climate", "climate change", "sustainability"},
		"Education":          {"education", "learning", "teaching"},
		"Health":             {"health", "healthcare", "medical"},
		"History":            {"history", "History", "historical"},
		"Economics":          {"economics", "economy", "Economics"},
		"Surveillance":       {"surveillance", "Surveillance", "mass surveillance", "spying"},
		"Palestine":          {"Palestine", "palestine", "Palestinian", "palestinian"},
		"Israel-Palestine":   {"Israel-Palestine", "israel-palestine", "Israel/Palestine"},
		"Censorship":         {"censorship", "Censorship", "free speech"},
		"Music":              {"music", "Music", "audio"},
		"Gaming":             {"gaming", "games", "video games", "videogames"},
		"Space":              {"space", "Space", "astronomy", "NASA", "nasa"},
		"Networking":         {"networking", "Networking", "network", "networks"},
		"Database":           {"database", "databases", "Database", "DB", "SQL", "sql"},
		"Containers":         {"containers", "docker", "Docker", "podman", "kubernetes", "k8s"},
		"Rust":               {"rust", "Rust", "rustlang"},
		"Go":                 {"go", "golang", "Golang"},
		"Encryption":         {"encryption", "Encryption", "cryptography", "crypto"},
		"Data":               {"data", "Data", "data science", "data analysis"},
	}

	// Filter to only groups where at least 2 variants actually exist as tags
	var validGroups []string
	totalMerges := 0
	for canonical, variants := range semanticGroups {
		allNames := append([]string{canonical}, variants...)
		var existing []string
		for _, name := range allNames {
			if _, ok := tagCounts[name]; ok {
				existing = append(existing, name)
			}
		}
		if len(existing) < 2 {
			continue
		}

		// Find the canonical name — prefer the one with most items
		best := canonical
		bestCount := tagCounts[canonical]
		for _, name := range existing {
			if tagCounts[name] > bestCount {
				best = name
				bestCount = tagCounts[name]
			}
		}

		var mergeFrom []string
		for _, name := range existing {
			if name != best {
				mergeFrom = append(mergeFrom, name)
			}
		}
		if len(mergeFrom) == 0 {
			continue
		}

		var quoted []string
		for _, name := range mergeFrom {
			quoted = append(quoted, fmt.Sprintf("%q", name))
		}
		validGroups = append(validGroups,
			fmt.Sprintf(`  {canonical: %q, variants: [%s]}`, best, strings.Join(quoted, ", ")))
		totalMerges += len(mergeFrom)
	}

	if len(validGroups) == 0 {
		log.Println("  No semantic tag merges needed")
		return nil
	}

	log.Printf("  Found %d semantic tag groups to merge (%d variant tags)", len(validGroups), totalMerges)

	script := fmt.Sprintf(`// Consolidate %d semantic tag groups (%d variant tags to merge)
var tagGroups = [
%s
];

var merged = 0;
for (let group of tagGroups) {
    for (let variant of group.variants) {
        let s = new Zotero.Search();
        s.addCondition('tag', 'is', variant);
        let ids = await s.search();
        for (let id of ids) {
            let item = await Zotero.Items.getAsync(id);
            if (!item) continue;
            item.removeTag(variant);
            item.addTag(group.canonical);
            await item.saveTx();
            merged++;
        }
    }
}
return merged + " tag assignments consolidated across " + tagGroups.length + " semantic groups";
`, len(validGroups), totalMerges, strings.Join(validGroups, ",\n"))

	return os.WriteFile(outDir+"/08-consolidate-semantic-tags.js", []byte(script), 0644)
}

// genDetectDeadLinks generates a script to check URLs for 404s and tag them.
func genDetectDeadLinks(conn *sql.DB, outDir string) error {
	var urlCount int
	conn.QueryRow(`
		SELECT COUNT(DISTINCT i.itemID) FROM items i
		JOIN itemData id ON i.itemID = id.itemID
		JOIN fields f ON id.fieldID = f.fieldID AND f.fieldName = 'url'
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND idv.value LIKE 'http%'
	`).Scan(&urlCount)

	log.Printf("  Will check %d items with URLs (runs in Zotero, may take a while)", urlCount)

	// This script runs entirely in Zotero's JS console — no pre-computation needed
	script := fmt.Sprintf(`// Check %d URLs for dead links (404, connection refused, timeout)
// WARNING: This may take several minutes. Run during low activity.

var items = await Zotero.Items.getAll(1);
var checked = 0, dead = 0, errors = 0;
var results = [];

for (let item of items) {
    if (item.isAttachment() || item.isNote()) continue;
    if (item.deleted) continue;

    let url;
    try { url = item.getField('url'); } catch(e) { continue; }
    if (!url || !url.startsWith('http')) continue;

    // Skip already-tagged items
    let tags = item.getTags().map(t => t.tag);
    if (tags.includes('dead-link') || tags.includes('link-checked')) continue;

    checked++;
    try {
        let resp = await Zotero.HTTP.request('HEAD', url, {
            timeout: 15000,
            successCodes: false  // Don't throw on non-2xx
        });

        if (resp.status === 404 || resp.status === 410 || resp.status === 451) {
            item.addTag('dead-link');
            item.addTag('http-' + resp.status);
            await item.saveTx();
            dead++;
            results.push(resp.status + " " + url);
        } else {
            item.addTag('link-checked');
            await item.saveTx();
        }
    } catch (e) {
        // Connection refused, timeout, DNS failure
        if (e.message && (e.message.includes('timed out') ||
            e.message.includes('refused') ||
            e.message.includes('not found') ||
            e.message.includes('NS_ERROR'))) {
            item.addTag('dead-link');
            item.addTag('connection-failed');
            await item.saveTx();
            dead++;
            results.push("CONN_ERR " + url);
        }
        errors++;
    }

    // Rate limit: 200ms between requests
    await Zotero.Promise.delay(200);

    // Progress every 100 items
    if (checked %% 100 === 0) {
        Zotero.log("Checked " + checked + " URLs, " + dead + " dead so far...");
    }
}

Zotero.log("Dead links found:");
for (let r of results) Zotero.log("  " + r);

return checked + " URLs checked, " + dead + " dead links tagged, " + errors + " errors";
`, urlCount)

	return os.WriteFile(outDir+"/09-detect-dead-links.js", []byte(script), 0644)
}
