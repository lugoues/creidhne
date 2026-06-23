// docs.go fetches systemd's man-page DocBook XML at the pinned tag and extracts
// per-directive documentation, so the generated CUE schema carries the upstream
// docs (as plain-text comments) without vendoring the XML. Network is needed
// only at generation time; the committed output is self-contained.
package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// docEntry is the documentation shared by one man-page <varlistentry> (a single
// directive, or a group like Before=/After= that share an entry & description).
type docEntry struct {
	page   string   // man page, e.g. "systemd.unit" (used to build the URL)
	anchor string   // first varname, e.g. "Before=" (the HTML anchor for the group)
	paras  []string // description paragraphs, flattened to plain text
}

// manPages are the systemd man pages documenting the directives this schema
// mirrors. Unit + Install live in systemd.unit; Service pulls from the service/
// exec/kill/cgroup contexts, matching the gperf macro expansion.
var manPages = []string{
	"systemd.unit",
	"systemd.service",
	"systemd.exec",
	"systemd.kill",
	"systemd.resource-control",
}

// loadDocs fetches each man page at the systemd git tag and returns a
// directive-name (no "=") -> docEntry map. tag is the git tag ("v257"); numVer
// is the bare version used in URLs and for &version; ("257").
func loadDocs(tag, numVer string) (map[string]docEntry, error) {
	docs := map[string]docEntry{}
	for _, page := range manPages {
		url := fmt.Sprintf("https://raw.githubusercontent.com/systemd/systemd/%s/man/%s.xml", tag, page)
		data, err := httpGet(url)
		if err != nil {
			return nil, fmt.Errorf("fetch %s docs (systemd %s): %w", page, tag, err)
		}
		for name, e := range parseManXML(data, page, numVer) {
			if _, ok := docs[name]; !ok { // first page that documents it wins
				docs[name] = e
			}
		}
	}
	return docs, nil
}

func httpGet(url string) ([]byte, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// xmlVarlistentry models the subset of a <varlistentry> we read: the <varname>s
// inside its <term>s (the directives being defined — NOT varnames mentioned in
// prose), and the <para> blocks of its <listitem> (captured raw, flattened
// later).
type xmlVarlistentry struct {
	Terms []struct {
		Varnames []string `xml:"varname"`
	} `xml:"term"`
	Listitem struct {
		Paras []struct {
			Inner string `xml:",innerxml"`
		} `xml:"para"`
	} `xml:"listitem"`
}

// parseManXML extracts every config directive (a <term> <varname> ending in "=")
// from a man page, keyed by directive name without the "=". Directives sharing a
// <varlistentry> share its docEntry. DecodeElement consumes nested
// <varlistentry>s (sub-option lists) inside the description, so only top-level
// directive entries are emitted.
func parseManXML(data []byte, page, numVer string) map[string]docEntry {
	out := map[string]docEntry{}
	dec := xml.NewDecoder(bytes.NewReader(replaceEntities(data, numVer)))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "varlistentry" {
			continue
		}
		var ve xmlVarlistentry
		if dec.DecodeElement(&ve, &se) != nil {
			continue
		}
		var names []string
		for _, t := range ve.Terms {
			for _, v := range t.Varnames {
				if v = strings.TrimSpace(v); strings.HasSuffix(v, "=") {
					names = append(names, v)
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		var paras []string
		for _, p := range ve.Listitem.Paras {
			if txt := flattenText(p.Inner); txt != "" {
				paras = append(paras, txt)
			}
		}
		e := docEntry{page: page, anchor: names[0], paras: paras}
		for _, n := range names {
			out[strings.TrimSuffix(n, "=")] = e
		}
	}
	return out
}

var (
	reEntity  = regexp.MustCompile(`&([a-zA-Z_][\w.-]*);`)
	reCiteref = regexp.MustCompile(`(?s)<citerefentry[^>]*>.*?<refentrytitle[^>]*>(.*?)</refentrytitle>.*?<manvolnum[^>]*>(.*?)</manvolnum>.*?</citerefentry>`)
	reTag     = regexp.MustCompile(`(?s)<[^>]+>`)
	reWS      = regexp.MustCompile(`\s+`)
)

// replaceEntities resolves &version; and rewrites systemd's custom DocBook
// entities (paths/constants from custom-entities.ent, which the standalone XML
// can't load) to their bare name, leaving the five standard XML entities for the
// decoder. Best-effort — the linked man page carries the authoritative text.
func replaceEntities(data []byte, numVer string) []byte {
	return reEntity.ReplaceAllFunc(data, func(m []byte) []byte {
		switch name := string(m[1 : len(m)-1]); name {
		case "amp", "lt", "gt", "quot", "apos":
			return m
		case "version":
			return []byte(numVer)
		default:
			return []byte(name)
		}
	})
}

// flattenText turns a paragraph's inner DocBook XML into one line of plain text:
// citerefentry -> "title(volnum)", remaining tags stripped, standard entities
// decoded, whitespace collapsed.
func flattenText(s string) string {
	s = reCiteref.ReplaceAllString(s, "$1($2)")
	s = reTag.ReplaceAllString(s, "")
	s = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
	return strings.TrimSpace(reWS.ReplaceAllString(s, " "))
}
