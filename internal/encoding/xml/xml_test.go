package xml_test

import (
	"iter"
	"strings"
	"testing"

	. "github.com/adoublef/cbz/internal/encoding/xml"
)

func TestURL(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		s := `
<rss xmlns:atom="http://www.w3.org/2005/Atom" version="2.0">
    <channel>
        <title>Title</title>
        <image>
            <url>https://example.com/path/to/image</url>
        </image>
    </channel>
</rss>
	`

		next, stop := iter.Pull2(Images(strings.NewReader(s)))
		defer stop()

		uri, err, _ := next()
		if err != nil {
			t.Fail()
		}
		if got, want := uri.String(), "https://example.com/path/to/image"; got != want {
			t.Fail()
		}

		_, _, more := next() // 2
		if more {
			t.Fail()
		}
	})
}
