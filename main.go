package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

var (
	fInArg  = flag.String("fin", "./urls", "input file")
	fOutArg = flag.String("fout", "./output.txt", "output file")
	bJson   = flag.Bool("json", false, "output format")
)

func init() {
	flag.Parse()
}

type file struct {
	name string
	perm os.FileMode
}

type rstat struct {
	Url      string              `json:"url,omitempty"`
	Err      error               `json:"error,omitempty"`
	Rcode    int                 `json:"code,omitempty"`
	Rheaders map[string][]string `json:"headers,omitempty"`
	Rlatency time.Duration       `json:"latency,omitempty"`
}

func (rs rstat) String() string {
	tml := `
	Code: %d
	Headers: 
		%+v
	Latency: %f sec
	`
	return fmt.Sprintf(tml, rs.Rcode, rs.Rheaders, rs.Rlatency.Seconds())

}

func main() {

	fIn := file{
		name: *fInArg,
		perm: 0666,
	}

	fOut := file{
		name: *fOutArg,
		perm: 0666,
	}

	if err := fIn.validatePath(); err != nil {
		panic(err)
	}

	if !fIn.exist() {
		panic("file doesn't exist")
	}

	if err := fOut.validatePath(); err != nil {
		panic(err)
	}

	fin, err := os.OpenFile(fIn.name, os.O_RDONLY, fIn.perm)
	if err != nil {
		panic(err)
	}

	defer fin.Close()

	scInput := bufio.NewScanner(os.Stdin)

	invm := "Please, enter either Yes or No:"
	fmt.Print("Would you like output in json. " + invm)

INPUT:
	for {

		for scInput.Scan() {

			in := scInput.Text()

			if scInput.Err() != nil {
				fmt.Print("Invalid input. " + invm)
				continue INPUT
			}

			switch in {
			case "Yes":
				*bJson = true
				break INPUT
			case "No":
				*bJson = false
				break INPUT
			default:
				fmt.Print("Invalid input. " + invm)
				continue INPUT
			}

		}

	}

	var bkpipe = make(chan *rstat, 1)

	scanner := bufio.NewScanner(fin)
	scanner.Split(bufio.ScanWords)

	var i int
	var t = time.Now()

	for ; scanner.Scan(); i++ {
		go fetch(scanner.Text(), bkpipe)
	}

	fout, err := os.OpenFile(fOut.name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fOut.perm)
	if err != nil {
		panic(err)
	}

	defer fout.Close()

	for ; i > 0; i-- {

		rs := <-bkpipe
		rs.Rlatency = time.Since(t)

		if *bJson {
			if bs, err := json.MarshalIndent(rs, "   ", "   "); err == nil {
				if _, err := fout.Write(bs); err != nil {
					log.Printf("failure when writing to file")
				}
			} else {
				log.Printf("failure when marshaling to json")
			}
		} else {
			rw := fmt.Sprintf("%v", rs)
			if rs.Err != nil {
				rw = fmt.Sprintf("\n%q - error: %v\n", rs.Url, rs.Err)
			}
			if _, err := fout.WriteString(rw); err != nil {
				log.Printf("failure when writing to file")
			}
		}

	}

	fmt.Print("Exit")

}

func (f *file) validatePath() error {

	fn := f.name
	re := regexp.MustCompile("^[a-zA-Z0-9._-].+")
	var err error

	switch {
	case strings.HasPrefix(fn, "/"):
		f.name = "." + fn
	case strings.HasPrefix(fn, "./"):
		f.name = fn
	case re.MatchString(fn):
		f.name = "./" + fn
	case strings.ContainsAny(fn, "#"):
		err = errors.New("filename contains inadmissible characters")
	default:

	}

	f.name = fn

	return err

}

func (f *file) exist() (isExist bool) {
	if _, err := os.Stat(f.name); !os.IsNotExist(err) {
		isExist = true
	}
	return
}

func (f *file) createIfNotExist() {

	fn := f.name

	if _, err := os.Stat(f.name); os.IsNotExist(err) {
		fn := strings.TrimSuffix(fn, "/"+path.Base(fn))
		err = os.MkdirAll(fn, 0666)
		if err != nil {
			panic(err)
		}
	}

	file, err := os.OpenFile(fn, os.O_RDWR|os.O_CREATE|os.O_TRUNC, f.perm)
	if err != nil {
		panic(err)
	}

	if err := file.Close(); err != nil {
		log.Printf("file.Close error: %v", err)
	}

}

func fetch(url string, bkpipe chan<- *rstat) {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		bkpipe <- &rstat{
			Url: url,
			Err: err,
		}
		return
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		bkpipe <- &rstat{
			Url: url,
			Err: err,
		}
		return
	}

	defer res.Body.Close()

	e := rstat{
		Url:      "",
		Err:      nil,
		Rheaders: res.Header,
		Rcode:    res.StatusCode,
	}

	select {
	case bkpipe <- &e:
		return
	case <-ctx.Done():
		bkpipe <- &rstat{
			Url: url,
			Err: errors.New("request timeout"),
		}
		return
	}

}
