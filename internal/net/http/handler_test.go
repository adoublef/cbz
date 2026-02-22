package http_test

import (
	"cmp"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	. "github.com/adoublef/cbz/internal/net/http"
	"github.com/zhyee/zipstream"
)

var numChapters, numImages int

func init() {
	flag.IntVar(&numChapters, "chapters", 1, "number of chapters")
	flag.IntVar(&numImages, "images", 1, "number of images")
}

func TestHandler(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		ctx := t.Context()

		imgC, imgURL := imgClient(t)
		apiC, apiURL := apiClient(t, imgURL, numChapters, numImages)

		tempDir := t.TempDir()
		testC, testURL := testClient(t, apiC, imgC, tempDir)

		url := fmt.Sprintf("%s/?series_url=%s", testURL, apiURL+"/series/1")
		req, err1 := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		res, err2 := testC.Do(req)
		ok(t, cmp.Or(err1, err2))
		equal(t, res.StatusCode, http.StatusOK) // stream means this is always going to be the case

		// stream zip
		zr := zipstream.NewReader(res.Body)
		for zr.Next() {
			e, err := zr.Entry()
			ok(t, err)
			// is another file inside
			equal(t, e.IsDir(), false)

			rc, err := e.Open()
			ok(t, err)

			// internal zip
			r := zipstream.NewReader(rc)
			for r.Next() {
				e, err := r.Entry()
				ok(t, err)
				equal(t, e.IsDir(), false)

				rc, err := e.Open()
				ok(t, err)
				n, err := io.Copy(io.Discard, rc)
				ok(t, err)
				equal(t, n, 86387)
				ok(t, rc.Close())
			}
			ok(t, rc.Close())
		}
		ok(t, zr.Err())

		// f, err := os.Create("output.zip")
		// ok(t, err)

		// _, err = io.Copy(f, res.Body)
		// ok(t, err)
		// ok(t, f.Close())

		ok(t, res.Body.Close())
	})
}

func testClient(t testing.TB, apiC, imgC *http.Client, tempDir string) (*http.Client, string) {
	t.Helper()

	s := httptest.NewServer(Handler(apiC, imgC, tempDir))
	t.Cleanup(s.Close)

	return s.Client(), s.URL
}

func ok(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", t.Name(), err)
	}
}

func equal[K comparable](t testing.TB, got, want K) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got=%v; want=%v", t.Name(), got, want)
	}
}

//go:embed testdata/*.html testdata/*.xml testdata/*.jpg
var embedFS embed.FS

func apiClient(t testing.TB, imgURL string, chapters, images int) (apiC *http.Client, apiURL string) {
	t.Helper()

	funcMap := template.FuncMap{
		// See https://stackoverflow.com/a/22716709
		"N":    func(n int) []struct{} { return make([]struct{}, n) },
		"sub":  func(a, b int) int { return a - b },
		"inc":  func(i int) int { return i + 1 },
		"iota": func(i int) string { return strconv.Itoa(i) },
		"join": func(sep string, s ...string) string { return strings.Join(s, sep) },
		"url": func(base string, s ...string) string {
			u, err := url.JoinPath(base, s...)
			if err != nil {
				t.Fatal(err)
			}
			return u
		},
	}

	series, err1 := template.New("series.html").Funcs(funcMap).ParseFS(embedFS, "testdata/series.html")
	rss, err2 := template.New("rss.xml").Funcs(funcMap).ParseFS(embedFS, "testdata/rss.xml")
	chapter, err3 := template.New("chapter.html").Funcs(funcMap).ParseFS(embedFS, "testdata/chapter.html")
	if err := cmp.Or(err1, err2, err3); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	// return series
	mux.HandleFunc("GET /series/{series}/full-chapter-list", func(w http.ResponseWriter, r *http.Request) {
		// some formatted string
		_, err := strconv.ParseUint(r.PathValue("series"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		data := struct {
			N       int
			BaseURL string
		}{
			N:       chapters,
			BaseURL: apiURL,
		}
		if err := series.Execute(w, data); err != nil {
			t.Fatal(err)
		}
	})
	// return series (rss)
	mux.HandleFunc("GET /series/{series}/rss", func(w http.ResponseWriter, r *http.Request) {
		// some formatted string
		_, err := strconv.ParseUint(r.PathValue("series"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if err := rss.Execute(w, nil); err != nil {
			t.Fatal(err)
		}
	})
	// return chapters
	mux.HandleFunc("GET /chapters/{chapter}/images", func(w http.ResponseWriter, r *http.Request) {
		// some formatted string
		id, err := strconv.ParseUint(r.PathValue("chapter"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		data := struct {
			N       int
			Chapter int
			BaseURL string
		}{
			N:       images,
			Chapter: int(id),
			BaseURL: imgURL,
		}
		if err := chapter.Execute(w, data); err != nil {
			t.Fatal(err)
		}
	})

	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s.Client(), s.URL
}

func imgClient(t testing.TB) (imgC *http.Client, imgURL string) {
	t.Helper()

	mux := http.NewServeMux()
	// return a file
	mux.HandleFunc("GET /{image}", func(w http.ResponseWriter, r *http.Request) {
		// parse path?

		f, err1 := embedFS.Open("testdata/image.jpg") // base64
		fi, err2 := f.Stat()
		if err := cmp.Or(err1, err2); err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		// Content-Length
		w.Header().Set("Content-Length", strconv.Itoa(int(fi.Size())))
		// Content-Type ?

		if r.Method != http.MethodHead {
			if _, err := io.Copy(w, f); err != nil {
				t.Fatal(err)
			}
		}
	})

	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s.Client(), s.URL
}
