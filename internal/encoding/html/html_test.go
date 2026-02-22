package html_test

import (
	"iter"
	"strings"
	"testing"

	. "github.com/adoublef/cbz/internal/encoding/html"
)

func TestAnchors(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		s := `
<a class="test" href="https://example.com/path/to/page/1"></a>
<div>
   	<a href="https://example.com/path/to/page/2" class="test"></a>
</div>
<div>
    <a href="https://example.com/path/to/page/3"></a>
</div>
	`

		next, stop := iter.Pull2(Anchors(strings.NewReader(s)))
		defer stop()

		uri, err, _ := next()
		if err != nil {
			t.Fail()
		}
		if got, want := uri.String(), "https://example.com/path/to/page/1"; got != want {
			t.Fail()
		}

		uri, err, _ = next()
		if err != nil {
			t.Fail()
		}
		if got, want := uri.String(), "https://example.com/path/to/page/2"; got != want {
			t.Fail()
		}

		uri, err, _ = next()
		if err != nil {
			t.Fail()
		}
		if got, want := uri.String(), "https://example.com/path/to/page/3"; got != want {
			t.Fail()
		}

		_, _, more := next() // 4
		if more {
			t.Fail()
		}
	})
}

func TestImages(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		s := `
   <a href="https://example.com/path/to/image/3"></a>
   <div>
      	<img src="https://example.com/path/to/image/2" class="test" />
   </div>
   <div>
       <img class="test" src="https://example.com/path/to/image/1" />
   </div>
	`

		next, stop := iter.Pull2(Images(strings.NewReader(s)))
		defer stop()

		uri, err, _ := next()
		if err != nil {
			t.Fail()
		}
		if got, want := uri.String(), "https://example.com/path/to/image/2"; got != want {
			t.Fail()
		}

		uri, err, _ = next()
		if err != nil {
			t.Fail()
		}
		if got, want := uri.String(), "https://example.com/path/to/image/1"; got != want {
			t.Fail()
		}

		_, _, more := next() // 2
		if more {
			t.Fail()
		}
	})
}
