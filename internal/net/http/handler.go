package http

import (
	"archive/zip"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime/trace"
	"strconv"
	"strings"
	"sync"

	"github.com/adoublef/cbz/internal/encoding/html"
	"golang.org/x/sync/errgroup"
)

func Handler(httpC *http.Client, tempDir string) http.Handler {
	// using a sync.Map for various http.Clients
	// key: domain
	// value: http.Client
	// See https://blog.wollomatic.de/posts/2025-01-28-go-tls-certificates/
	return handleSeries(httpC, tempDir)
}

func handleSeries(httpC *http.Client, tempDir string) handlerFunc {
	parse := func(w http.ResponseWriter, r *http.Request) (*url.URL, error) {
		parsed, err := url.Parse(r.URL.Query().Get("series_url"))
		if err != nil {
			return nil, httpError(http.StatusBadRequest)
		}
		// ensure path is formatted explicitly as /[series]/[id]
		path := strings.TrimPrefix(parsed.Path, "/")
		first, rest, more := strings.Cut(path, "/")
		if !more || first == "" || rest == "" || strings.Contains(rest, "/") {
			return nil, httpError(http.StatusUnprocessableEntity)
		}
		return parsed, nil
	}

	writeTo := func(zw *zip.Writer, rfs *os.Root, filename string) (err error) {
		defer rfs.Remove(filename)

		f, err := rfs.Open(filename)
		if err != nil {
			return err
		}
		defer f.Close()

		fi, err1 := f.Stat()
		zh, err2 := zip.FileInfoHeader(fi)
		zh.Method = zip.Deflate // needed for the streaming
		w, err3 := zw.CreateHeader(zh)
		if err := cmp.Or(err1, err2, err3); err != nil {
			return err
		}

		_, err = io.Copy(w, f)
		return err
	}

	zipReader := func(ctx context.Context, series *url.URL, rfs *os.Root) io.ReadCloser {
		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(5)

		chapters := make(chan *url.URL)
		g.Go(func() error {
			defer close(chapters)

			uri := series.JoinPath("full-chapter-list")
			req, err1 := http.NewRequestWithContext(ctx, http.MethodGet, uri.String(), nil)
			res, err2 := httpC.Do(req)
			if err := cmp.Or(err1, err2); err != nil {
				return err
			}
			defer res.Body.Close()

			if c := res.StatusCode; c != http.StatusOK {
				return fmt.Errorf("failed query returned %d status code", c)
			}

			// if there are no chapters, something went wrong
			// return with an error
			var count int
			for url, err := range html.Anchors(res.Body) {
				if err != nil {
					return err
				}
				// i wanna spawn processes to fetch these pages concurrently
				// verify that url is correct before sending off
				//
				// ensure path is formatted explicitly as /[chapters]/[id]
				path := strings.TrimPrefix(url.Path, "/")
				first, rest, more := strings.Cut(path, "/")
				if !more || first == "" || rest == "" || strings.Contains(rest, "/") {
					return fmt.Errorf("invalid chapter url: (path: %q)", url.Path)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case chapters <- url: // can be assured we have two parts only
					count++
				}
			}
			if count == 0 {
				return fmt.Errorf("failed to find any chapters")
			}

			return nil
		})

		type msg struct {
			url   *url.URL
			reply chan<- string
			done  func()
		}
		images := make(chan msg)

		names := make(chan string)
		g.Go(func() error {
			defer func() { close(images); close(names) }()

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(1)

			for chapter := range chapters {
				g.Go(func() error {
					// create a temp directory for the chapter?
					// spin up two routines
					g, ctx := errgroup.WithContext(ctx)
					// 1) handle the initial chapter request
					var reply = make(chan string) // they all send to this channel
					var wg sync.WaitGroup         // safe close since i only want to use one channel

					g.Go(func() error {
						defer func() { wg.Wait(); close(reply) }() // error safe?

						uri := chapter.JoinPath("images")
						// we need the url Values added here
						req, err1 := http.NewRequestWithContext(ctx, http.MethodGet, uri.String(), nil)
						res, err2 := httpC.Do(req)
						if err := cmp.Or(err1, err2); err != nil {
							return err
						}
						defer res.Body.Close()
						if c := res.StatusCode; c != http.StatusOK {
							return fmt.Errorf("failed query returned %d status code", c)
						}

						var count int
						for url, err := range html.Images(res.Body) {
							if err != nil {
								return err
							}
							// ensure the name is valid format /
							base := path.Base(url.Path)
							var cn, in int
							var ext string
							_, err := fmt.Sscanf(base, "%d-%d.%s", &cn, &in, &ext)
							if err != nil {
								return err
							}
							// n, err == 3, <nil>
							select {
							case <-ctx.Done():
								return ctx.Err()
							case images <- msg{url, reply, wg.Done}:
								wg.Add(1)
							}
							count++
						}
						if count == 0 {
							return fmt.Errorf("failed to find any images")
						}
						return nil
					})
					// 2) gets a response and creates a zip
					g.Go(func() error {
						// temp zip file that we can now feed data into
						name := path.Base(chapter.Path) + ".zip"

						f, err := rfs.Create(name) // for now just use the id, find a way to get the actual position
						if err != nil {
							return err
						}
						defer f.Close()

						zw := zip.NewWriter(f)
						defer zw.Flush()

						for filename := range reply {
							if err := writeTo(zw, rfs, filename); err != nil {
								return err
							}
						}
						if err := zw.Close(); err != nil {
							return err
						}
						// Todo stream to the client
						select {
						case <-ctx.Done():
							return ctx.Err()
						case names <- name: // the final cbz
						}
						return nil
					})

					return g.Wait()
				})
			}

			return g.Wait()
		})

		g.Go(func() error {
			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(1)
			for msg := range images {
				g.Go(func() error {
					defer msg.done() // no likey

					name := path.Base(msg.url.Path)

					f, err := rfs.Create(name)
					if err != nil {
						return err
					}
					defer f.Close()

					// query image and download the file
					req, err1 := http.NewRequestWithContext(ctx, http.MethodGet, msg.url.String(), nil)
					res, err2 := httpC.Do(req)
					if err := cmp.Or(err1, err2); err != nil {
						return err
					}
					defer res.Body.Close()
					if c := res.StatusCode; c != http.StatusOK {
						return fmt.Errorf("failed query returned %d status code", c)
					}

					// TODO 1) Content-Type says png, we could verify this
					// 1) Content-Length for limiting the size when copying
					size, err := strconv.ParseUint(res.Header.Get("Content-Length"), 10, 64)
					if err != nil {
						return err
					}
					mbr := http.MaxBytesReader(nil, res.Body, int64(size))
					n, err := io.Copy(f, mbr)
					if err != nil {
						return err
					} else if n == 0 {
						return fmt.Errorf("failed to read data")
					}

					select {
					case <-ctx.Done():
						return ctx.Err()
					case msg.reply <- name: // signal that we are done
					}
					return nil
				})
			}
			return g.Wait()
		})

		pr, pw := io.Pipe()
		g.Go(func() error {
			// collect all previous chapters (+ cover.jpg)
			zw := zip.NewWriter(pw)
			defer zw.Flush()
			for filename := range names {
				if err := writeTo(zw, rfs, filename); err != nil {
					return err
				}
			}
			return zw.Close()
		})
		go func() { pw.CloseWithError(g.Wait()) }()
		return pr
	}

	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, task := trace.NewTask(r.Context(), "handleSeries")
		defer task.End()

		series, err := parse(w, r)
		if err != nil {
			return err
		}

		// make a temp directory for the series
		tempDir, err := os.MkdirTemp(tempDir, "")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tempDir)

		rfs, err := os.OpenRoot(tempDir)
		if err != nil {
			return err
		}
		defer rfs.Close()

		zr := zipReader(ctx, series, rfs)
		defer zr.Close()

		_, err = io.Copy(w, zr)
		return err
	}
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (h handlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h(w, r)
	if err == nil {
		return
	}

	if err, ok := errors.AsType[httpError](err); ok {
		http.Error(w, err.Error(), int(err))
		return
	}
	log.Printf("unexpected error occured: %v\n", err)
}

type httpError int

func (e httpError) Error() string { return http.StatusText(int(e)) }
