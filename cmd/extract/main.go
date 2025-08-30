package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Heuristics (tweakable via flags if you want later)
var (
	footnoteFrac   = 0.82 // drop lines whose top Y >= 82% of page height
	leftMarginFrac = 0.20 // a verse number is near left margin if x0 <= 20% width
	superRisePx    = 5    // words whose top is above (lineTop - superRisePx) are "superscripts"
)

// Flags
var (
	inFile     = flag.String("in", "2015.223782.The-Dhammapada_hocr.html", "hOCR HTML file")
	textsCSV   = flag.String("texts", "texts.csv", "output CSV for texts (id,label,text_body)")
	mapCSV     = flag.String("text_verses", "text_verses.csv", "output CSV for text_verses (text_id,verse_number)")
	pageMin    = flag.Int("page-min", 60, "min page (inclusive) to parse (hOCR ppageno)")
	pageMax    = flag.Int("page-max", 96, "max page (inclusive) to parse (hOCR ppageno)")
	composites = flag.String("pairs", "58-59,104-105,153-154,195-196,229-230,256-257,268-269,271-272", "comma-separated composite pairs A-B")
)

type bbox struct{ x0, y0, x1, y1 int }

var reBBox = regexp.MustCompile(`bbox\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)`)
var rePageNo = regexp.MustCompile(`ppageno\s+(\d+)`)
var reInt = regexp.MustCompile(`^\d+$`)

func parseBBox(title string) (bbox, bool) {
	m := reBBox.FindStringSubmatch(title)
	if m == nil {
		return bbox{}, false
	}
	return bbox{atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4])}, true
}
func parsePageNo(title string) (int, bool) {
	m := rePageNo.FindStringSubmatch(title)
	if m == nil {
		return 0, false
	}
	return atoi(m[1]), true
}
func atoi(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}

type word struct {
	x int
	t string
}
type line struct {
	b  bbox
	ws []word
}

type verse struct {
	num  int
	text string
}

type textEnt struct {
	id    int
	label string
	body  string
}

// ---- main extraction

func main() {
	flag.Parse()

	doc, err := goquery.NewDocumentFromReader(mustOpen(*inFile))
	if err != nil {
		log.Fatalf("parse hOCR: %v", err)
	}

	// 1) Iterate pages
	// type verse struct {
	// 	num  int
	// 	text string
	// }
	verses := make(map[int]string)

	doc.Find(".ocr_page").Each(func(_ int, pg *goquery.Selection) {
		title := getAttr(pg, "title")
		pb, ok := parseBBox(title)
		if !ok {
			return
		}
		pp, ok := parsePageNo(title)
		// If no ppageno, we conservatively include; otherwise apply window
		if ok && (pp < *pageMin || pp > *pageMax) {
			return
		}

		pHeight := pb.y1 - pb.y0
		pWidth := pb.x1 - pb.x0
		footCut := pb.y0 + int(float64(pHeight)*footnoteFrac)
		leftCut := pb.x0 + int(float64(pWidth)*leftMarginFrac)

		// collect cleaned lines
		var lines []line
		pg.Find(".ocr_line").Each(func(_ int, ln *goquery.Selection) {
			lb, ok := parseBBox(getAttr(ln, "title"))
			if !ok {
				return
			}
			// drop footnote region
			if lb.y0 >= footCut {
				return
			}
			// collect words, skip superscripts
			ws := make([]word, 0, 8)
			ln.Find(".ocrx_word").Each(func(_ int, w *goquery.Selection) {
				wb, ok := parseBBox(getAttr(w, "title"))
				if !ok {
					return
				}
				text := strings.TrimSpace(w.Text())
				if text == "" {
					return
				}
				// superscript: top sits above line top by > superRisePx
				if wb.y0 < lb.y0-superRisePx {
					return
				}
				ws = append(ws, word{x: wb.x0, t: text})
			})
			if len(ws) == 0 {
				return
			}
			// order words left->right
			sort.Slice(ws, func(i, j int) bool { return ws[i].x < ws[j].x })
			lines = append(lines, line{b: lb, ws: ws})
		})

		// stitch verses
		var currentNum *int
		var buf []string

		flush := func() {
			if currentNum != nil && len(buf) > 0 {
				n := *currentNum
				txt := strings.TrimSpace(joinWithSpaces(buf))
				if prev, ok := verses[n]; ok {
					// same verse continued on next line/page
					verses[n] = strings.TrimSpace(prev + " " + txt)
				} else {
					verses[n] = txt
				}
			}
			currentNum = nil
			buf = buf[:0]
		}

		for _, ln := range lines {
			first := ln.ws[0].t
			firstX := ln.ws[0].x
			if firstX <= leftCut && reInt.MatchString(first) {
				// new verse start
				flush()
				vn := atoi(first)
				currentNum = &vn
				// rest of the words form the start of verse
				for _, w := range ln.ws[1:] {
					buf = append(buf, w.t)
				}
			} else {
				// continuation of current verse (if any)
				for _, w := range ln.ws {
					buf = append(buf, w.t)
				}
			}
		}
		flush()
	})

	// 2) Build text entities (singles + composites)
	// type textEnt struct {
	// 	id    int
	// 	label string
	// 	body  string
	// }
	var entities []textEnt
	var mappings [][2]int // (text_id, verse_number)

	// load composite pairs from flag like "58-59,104-105"
	pairs := parsePairs(*composites)

	// build a set of verse numbers already consumed (e.g., the second element of a pair)
	consumed := map[int]bool{}

	// First, add composites where both sides exist; if only one side exists, we still create a composite from the one we have
	for _, p := range pairs {
		a, b := p[0], p[1]
		ta, oka := verses[a]
		tb, okb := verses[b]
		if !oka && !okb {
			continue
		}
		label := fmt.Sprintf("%d–%d", a, b)
		body := strings.TrimSpace(strings.Join(filterNonEmpty([]string{ta, tb}), " "))
		entID := len(entities) + 1
		entities = append(entities, textEnt{id: entID, label: label, body: body})
		mappings = append(mappings, [2]int{entID, a})
		mappings = append(mappings, [2]int{entID, b})
		consumed[a] = true
		consumed[b] = true
	}

	// Then add remaining single verses
	keys := make([]int, 0, len(verses))
	for k := range verses {
		if !consumed[k] {
			keys = append(keys, k)
		}
	}
	sort.Ints(keys)
	for _, k := range keys {
		entID := len(entities) + 1
		entities = append(entities, textEnt{id: entID, label: strconv.Itoa(k), body: verses[k]})
		mappings = append(mappings, [2]int{entID, k})
	}

	// 3) Write CSVs
	if err := writeTextsCSV(*textsCSV, entities); err != nil {
		log.Fatalf("write texts csv: %v", err)
	}
	if err := writeMapCSV(*mapCSV, mappings); err != nil {
		log.Fatalf("write text_verses csv: %v", err)
	}

	log.Printf("Extracted %d text entities; wrote %s and %s", len(entities), *textsCSV, *mapCSV)
}

// ---- helpers

func mustOpen(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open %s: %v", path, err)
	}
	return f
}
func getAttr(s *goquery.Selection, key string) string {
	if v, ok := s.Attr(key); ok {
		return v
	}
	return ""
}
func joinWithSpaces(tokens []string) string {
	// fix common OCR spacing like " ,", " .", " ;"
	s := strings.Join(tokens, " ")
	s = regexp.MustCompile(`\s+([,.;:!?])`).ReplaceAllString(s, "$1")
	return s
}

func parsePairs(spec string) [][2]int {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	var out [][2]int
	for _, chunk := range strings.Split(spec, ",") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		parts := strings.Split(chunk, "-")
		if len(parts) != 2 {
			log.Fatalf("bad pair %q (want A-B)", chunk)
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		b, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			log.Fatalf("bad pair %q: %v %v", chunk, err1, err2)
		}
		out = append(out, [2]int{a, b})
	}
	return out
}

func filterNonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func writeTextsCSV(path string, ents []textEnt) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	// header: id,label,text_body
	fmt.Fprintln(w, "id,label,text_body")
	for _, e := range ents {
		// very simple CSV escaping of quotes
		label := strings.ReplaceAll(e.label, `"`, `""`)
		body := strings.ReplaceAll(e.body, `"`, `""`)
		fmt.Fprintf(w, "%d,%q,%q\n", e.id, label, body)
	}
	return w.Flush()
}

func writeMapCSV(path string, maps [][2]int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	// header: text_id,verse_number
	fmt.Fprintln(w, "text_id,verse_number")
	for _, m := range maps {
		fmt.Fprintf(w, "%d,%d\n", m[0], m[1])
	}
	return w.Flush()
}
