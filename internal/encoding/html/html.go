package html

import (
	"io"
	"iter"
	"net/url"

	"golang.org/x/net/html"
)

// Anchors iterates a given reader for valid urls.
func Anchors(r io.Reader) iter.Seq2[*url.URL, error] {
	// https://drstearns.github.io/tutorials/tokenizing/
	z := html.NewTokenizer(r)
	return func(yield func(*url.URL, error) bool) {
	LOOP:
		for {
			switch tt := z.Next(); tt {
			case html.ErrorToken:
				err := z.Err()
				if err == io.EOF {
					return // stop
				}
				if err != nil && !yield(nil, err) {
					return
				}
			case html.StartTagToken: // <a>
				tn, more := z.TagName()
				if len(tn) == 1 && tn[0] == 'a' && more {
					var key, val []byte
					for more {
						key, val, more = z.TagAttr()
						if string(key) == "href" && len(val) > 0 {
							if !yield(url.Parse(string(val))) {
								return
							}
							continue LOOP
						}
					}
				}
			}
		}
	}
}

// Images iterates a given reader for valid urls.
func Images(r io.Reader) iter.Seq2[*url.URL, error] {
	// https://drstearns.github.io/tutorials/tokenizing/
	z := html.NewTokenizer(r)
	return func(yield func(*url.URL, error) bool) {
	LOOP:
		for {
			switch tt := z.Next(); tt {
			case html.ErrorToken:
				err := z.Err()
				if err == io.EOF {
					return // stop
				}
				if err != nil && !yield(nil, err) {
					return
				}
			case html.SelfClosingTagToken: // <img/>
				tn, more := z.TagName()
				if len(tn) == 3 && string(tn) == "img" && more {
					var key, val []byte
					for more {
						key, val, more = z.TagAttr()
						if string(key) == "src" && len(val) > 0 {
							if !yield(url.Parse(string(val))) {
								return
							}
							continue LOOP
						}
					}
				}
			}
		}
	}
}
