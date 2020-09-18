// The bookmark command archives URLs for future reference.
//
// Bookmark saves a web page corresponding to a URL.
//
// See also: RFC 7089
package main

import (
	"bytes"
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
	// save bookmarks to $HOME/.bookmark
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

// readBookmarkDB reads the list of bookmarks from a file
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
