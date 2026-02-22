package xml

import (
	"encoding/xml"
	"io"
	"iter"
	"net/url"
)

func Images(r io.Reader) iter.Seq2[*url.URL, error] {
	dec := xml.NewDecoder(r)
	return func(yield func(*url.URL, error) bool) {
		depth := 0
		for {
			t, err := dec.Token()
			if err == io.EOF {
				// // have we found url?
				// if u == nil {
				// 	return nil, errors.New("url not found")
				// }
				return
			}
			if err != nil && !yield(nil, err) {
				return
			}
			switch t := t.(type) {
			// StartElement, EndElement, CharData, Comment, ProcInst, or Directive.
			case xml.StartElement:
				if t.Name.Local == "image" || t.Name.Local == "url" {
					depth++
				}
			case xml.EndElement:
				if t.Name.Local == "image" || t.Name.Local == "url" {
					depth--
				}
			case xml.CharData:
				if depth == 2 {
					if !yield(url.Parse(string(t))) {
						return
					}
				}
			}
		}
	}
}
