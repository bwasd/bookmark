// The bookmark command archives URLs for future reference.
//
// Bookmark saves a web page corresponding to a URL.
//
// See also, RFC 7089.

// TODO: when adding a bookmark check if the URL is archived in (for example)
// Internet Archive Wayback Machine. If the URL is not archived or out of date,
// archive the URL.
// TODO: implement support for the Memento Protocol (RFC 7089).
// TODO: cleanurl by canonicalization:
//	- remove locale indicators. Example:
// https://en.wikipedia.org/wiki/Foo => https://wikipedia.org/wiki/Foo -
// sanitize URL of tracking parameters
// (https://en.wikipedia.org/wiki/UTM_parameters) XXX: striphtml by removing
// extraneous markup, CSS, Javascript and cruft
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

var (
	bookmarkDB = filepath.Join(os.Getenv("HOME"), ".bookmark")
	db         *BookmarkDB
)

type BookmarkDB struct {
	file      string
	data      []byte
	bookmarks map[string]Bookmark
}

type Bookmark struct {
	url []byte
}

func readBookmarkDB(file string) *BookmarkDB {
	b := &BookmarkDB{
		file:      file,
		bookmarks: make(map[string]Bookmark),
	}

	data, err := ioutil.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return b
		}
		log.Fatal(err)
	}

	b.data = data
	lines := bytes.SplitAfter(data, []byte("\n"))
	for _, line := range lines {
		f := bytes.TrimSuffix(line, []byte("\n"))
		if len(f) == 0 {
			continue
		}
		var bm Bookmark
		bm.url = f
		b.bookmarks[string(bm.url)] = bm
	}
	return b
}

func list() {
	var bookmarks []string
	for _, bm := range db.bookmarks {
		bookmarks = append(bookmarks, string(bm.url))
	}
	sort.Strings(bookmarks)
	for _, bm := range bookmarks {
		fmt.Println(bm)
	}
}

type Closest struct {
	Available bool   `json:"available"`
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
}

type ArchivedSnapshots struct {
	Closest Closest `json:"closest"`
}

// checkAvailable checks whether or not an archive of a webpage corresponding to
// a URL is available in the Wayback Machine.
//
// By default, a response returning the most recent snapshot is returned. If a
// timestamp is given in the format YYYYMMDDhhmmss (1-14 digits), the closest
// snapshot is returned.
//
// See Wayback Availability JSON API: https://archive.org/help/wayback_api.php
func checkAvailable(urlstr, ts string) {
	v := make(url.Values)
	v.Set("url", urlstr)
	if ts != "" {
		v.Set("timestamp", ts)
	}

	url := "http://archive.org/wayback/available?" + v.Encode()
	res, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	var r ArchivedSnapshots
	if err := json.Unmarshal(data, &r); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%v", r)
}

func savePage(urlstr string) error {
	client := http.Client{
		Timeout: time.Duration(20 * time.Second),
	}

	retry := 0
	const maxRetry int = 3
	for retry < maxRetry {
		req, err := http.NewRequest("GET", urlstr, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		_, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading response body: %v", err)
		}

		if resp.StatusCode >= 400 {
			if resp.StatusCode == 404 {
				return fmt.Errorf("resource not found: %v", urlstr)
			}

			// TODO: according to MDN and RFC 7231 section-7.1.3, a Retry-After
			// response header may be sent with 301 (Moved Permanently), 429
			// (Too Many Requests) and 503 (Service Unavailable) but some
			// services by convention, return rate-limit headers prefixed by
			// "X-".
			if resp.StatusCode == 429 || resp.StatusCode == 503 {
				n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
				if n > 0 {
					t := time.Unix(int64(n), 0)
					time.Sleep(t.Sub(time.Now()) + 1*time.Minute)
					retry++
					continue
				}
			}
		}

		// TODO: return resolved URL after redirection
		if resp.StatusCode/100 == 3 {
			nurl, err := resp.Location()
			if err != nil {
				return fmt.Errorf("resolving redirect: %v", urlstr)
			}
			urlstr = nurl.String()
		}

		if resp.StatusCode/500 == 5 {
			if retry == maxRetry {
				log.Fatal("max retries exceeded")
			}
			retry++
			continue
		}
		break
	}

	return nil
}

func saneURL(u *url.URL) error {
	if u.Port() != "" {
		return nil
	}
	return nil
}

func add(urlstr string) {
	u, err := url.Parse(urlstr)
	if err != nil {
		log.Fatalf("parsing URL: %v", urlstr)
	}
	urlstr = u.String()
	if _, dup := db.bookmarks[urlstr]; dup {
		log.Fatalf("duplicate: %v", urlstr)
	}

	if err := savePage(urlstr); err != nil {
		log.Fatal(err)
	}

	f, err := os.OpenFile(bookmarkDB, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("opening bookmark db: %v", err)
	}

	if _, err := f.Write([]byte(urlstr + "\n")); err != nil {
		log.Fatalf("adding bookmark: %v", err)
	}
	if err := f.Close(); err != nil {
		log.Fatalf("adding bookmark: %v", err)
	}
}

var (
	flagList = flag.Bool("list", false, "list bookmarks")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: bookmark [-list] [url...]\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	log.SetPrefix("bookmark: ")
	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()
	db = readBookmarkDB(bookmarkDB)

	if *flagList {
		if flag.NArg() > 0 {
			usage()
		}
		list()
		return
	}

	if len(flag.Args()) > 1 {
		fmt.Fprintf(os.Stderr, "too many arguments\n")
		usage()
	}
	url := flag.Arg(0)
	add(url)
}
